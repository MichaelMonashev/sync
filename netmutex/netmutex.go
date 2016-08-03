package netmutex

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
//	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Коды ошибок
var (
	ErrNoNodes        = errors.New("No working nodes.")
	ErrTimeout        = errors.New("Timeout exceeded.")
	ErrBusy           = errors.New("Servers busy.")
	ErrTooMuchRetries = errors.New("Too much retries.")
	ErrLongKey        = errors.New("Key too long.")
)

var (
	errWrongResponse = errors.New("Wrong response.")
	errLockIsNil     = errors.New("Try to unlock nil lock.")
)

type response struct {
	id          commandID         // уникальный номер команды
	nodes       map[uint64]string // список нод при OPTIONS
	nodeID      uint64            // адрес ноды при REDIRECT
	description string            // описание ошибки при ERROR
	code        byte              // код команды. Должно быть в хвосте структуры, ибо портит выравнивание всех остальных полей
}

type command struct {
	id          commandID      // уникальный номер команды
	key         string         // ключ
	ttl         time.Duration  // на сколько лочим ключ
	timeout     time.Duration  // за какое время надо попытаться выполнить команду
	respChan    chan error     // канал, в который пишется ответ от ноды
	sendChan    chan *node     // канал, в который пишется нода, на которую надо послать запрос
	processChan chan *response // канал, в который пишется ответ от ноды, который надо обработать
	retries     int32          // количество запросов к нодам, прежде чем вернуть ошибку
	netmutex    *NetMutex      // ссылка на клиента для чтения списка нод и retries
	currentNode *node          // нода, которая обработывает текущего запрос
	timer       *time.Timer    // таймер текущего запроса
	code        byte           // код команды. Должно быть в хвосте структуры, ибо портит выравнивание всех остальных полей
}

// коды команд
const (
	CONNECT = iota // запрос списка нод и номера соединения
	OPTIONS        // ответ на CONNECT-запрос со списком нод и номером соединения

	PING // пустой запрос для определения rtt и mtu
	PONG // пустой ответ для определения rtt и mtu

	LOCK   // запрос заблокировать ключ
	UNLOCK // запрос снять блокировку с ранее заблокированного ключа
)

// коды ответов
const (
	OK       = iota + 100 // ответ: всё хорошо
	REDIRECT              // ответ: повторить запрос на другую ноду
	TIMEOUT               // ответ: не удалось выполнить команду. таймаут наступил раньше
	BUSY                  // ответ: сервер перегружен
	ERROR                 // ответ: ошибка
)

//func code2string(code byte) string {
//	switch code {
//	case CONNECT:
//		return "CONNECT"
//	case OPTIONS:
//		return "OPTIONS"
//	case LOCK:
//		return "LOCK"
//	case UNLOCK:
//		return "UNLOCK"
//	case OK:
//		return "OK"
//	case REDIRECT:
//		return "REDIRECT"
//	case TIMEOUT:
//		return "TIMEOUT"
//	case BUSY:
//		return "BUSY"
//	case ERROR:
//		return "ERROR"
//	case PING:
//		return "PING"
//	case PONG:
//		return "PONG"
//	default:
//		fmt.Fprintln(os.Stderr, "Wrong code:", fmt.Sprint(code))
//		return ""
//	}
//}

type commandID struct {
	nodeID       uint64
	connectionID uint64
	requestID    uint64
}

type node struct {
	id    uint64
	fails uint32
	mtu   uint32
	rtt   uint32
	addr  string
	conn  *net.UDPConn
}

func (node *node) fail() {
	atomic.AddUint32(&node.fails, 1)
}

func (node *node) ok() {
	atomic.StoreUint32(&node.fails, 0)
}

func (node *node) timeout() time.Duration { // ToDo сделать таймаут адаптивным, т.е. считать таймаут на основании нескольких последних периодов между запросом и ответом
	switch atomic.LoadUint32(&node.fails) {
	case 0:
		return 100 * time.Millisecond
	case 1:
		return 200 * time.Millisecond
	case 2:
		return 400 * time.Millisecond
	case 3:
		return 800 * time.Millisecond
	default:
		return 1000 * time.Millisecond
	}
}

type nodes struct {
	sync.Mutex
	m       map[uint64]*node
	current *node
}

type Options struct {
	TTL             time.Duration
	Timeout         time.Duration
	Retries         int32
	ReadBufferSize  int
	WriteBufferSize int
}

type NetMutex struct {
	nextCommandID   commandID     // должна быть первым полем в структуре, иначе может быть неверное выравнивание и atomic перестанет работать
	ttl             time.Duration // значение по умолчанию для Lock(), Unlock()
	timeout         time.Duration // значение по умолчанию для Timeout
	retries         int32
	readBufferSize  int
	writeBufferSize int
	done            chan struct{}
	doneErr         chan error
	responses       chan *response
	nodes           *nodes
	workingCommands *workingCommands
}

func (netmutex *NetMutex) commandID() commandID {
	return commandID{
		nodeID:       netmutex.nextCommandID.nodeID,
		connectionID: netmutex.nextCommandID.connectionID,
		requestID:    atomic.AddUint64(&netmutex.nextCommandID.requestID, 1),
	}
}

func (netmutex *NetMutex) node() (*node, error) {
	netmutex.nodes.Lock()
	defer netmutex.nodes.Unlock()

	if netmutex.nodes.current != nil {
		if atomic.LoadUint32(&netmutex.nodes.current.fails) == 0 { // ToDo переписать. при множестве потерь пакетов это условие редко срабатывает и каждый раз делается перебор нод
			return netmutex.nodes.current, nil
		}
	}

	var bestValue uint32
	var bestNode *node
	for _, n := range netmutex.nodes.m {
		// пропускаем несоединившиеся ноды и текущую ноду
		if n.conn != nil && netmutex.nodes.current != n {

			if bestNode == nil {
				bestValue = atomic.LoadUint32(&n.fails)
				bestNode = n
			} else {
				curValue := atomic.LoadUint32(&n.fails)
				if bestValue > curValue {
					bestValue = curValue
					bestNode = n
				}
			}
		}
	}

	if bestNode != nil {
		netmutex.nodes.current = bestNode
		return netmutex.nodes.current, nil
	}

	return nil, ErrNoNodes
}

func (netmutex *NetMutex) nodeByID(nodeID uint64) *node {
	netmutex.nodes.Lock()
	defer netmutex.nodes.Unlock()

	if node, ok := netmutex.nodes.m[nodeID]; ok {
		return node
	}

	return nil
}

// заблокировать, если не было заблокировано ранее
//func (netmutex *NetMutex) LockIfUnlocked(key string) (*Lock, error) {
//	return nil, errors.New("LockIfUnlocked() has not yet implemented.")
//}

// заблокировать все или ниодного.
//func (netmutex *NetMutex) LockEach(keys []string) ([]*Lock, error) {
//	return nil, errors.New("LockEach() has not yet implemented.")
//}

// заблокировать всё, что получится.
//func (netmutex *NetMutex) LockAny(keys []string) ([]*Lock, error) {
//	return nil, errors.New("LockAny() has not yet implemented.")
//}

// заблокировать на чтение
//func (netmutex *NetMutex) RLock(key string) (*Lock, error) {
//	return netmutex.Lock(key) // ToDo переделать
//}

// снять все блокировки
//func (netmutex *NetMutex) UnlockAll() error {
//	return errors.New("UnlockAll() has not yet implemented.")
//}

// за время timeout установить блокировку ключа key на время ttl
// Lock(key string)
func (netmutex *NetMutex) Lock(key string) (*Lock, error) {

	if len(key) > 255 {
		return nil, ErrLongKey
	}

	timeout := netmutex.timeout
	ttl := netmutex.ttl
	commandID := netmutex.commandID()

	err := netmutex.runCommand(key, commandID, ttl, LOCK, timeout)
	if err != nil {
		return nil, err
	}

	lock := acquireLock()

	lock.key = key
	lock.netmutex = netmutex
	lock.commandID = commandID
	lock.timeout = timeout

	return lock, nil

}

func acquireLock() *Lock {
	l := lockPool.Get()
	if l == nil {
		return &Lock{}
	}
	return l.(*Lock)
}

func releaseLock(l *Lock) {
	lockPool.Put(l)
}

var lockPool sync.Pool

type Lock struct {
	key       string
	netmutex  *NetMutex
	commandID commandID
	timeout   time.Duration
}

// Возвращает ключ, по которому произошла блокировка. Удоббен в LochEach() и LockAny()
//func (lock *Lock) Key() string {
//	return lock.key
//}

// за время timeout снять ранее установленную блокировку
func (netmutex *NetMutex) Unlock(lock *Lock) error {
	if lock == nil {
		return errLockIsNil
	}

	defer releaseLock(lock)

	return lock.netmutex.runCommand(lock.key, lock.commandID, 0, UNLOCK, lock.timeout)
}

func writeWithTimeout(conn *net.UDPConn, command *command, timeout time.Duration) error {
	err := conn.SetWriteDeadline(time.Now().Add(timeout))
	if err != nil {
		return err
	}

	err = write(conn, command)
	if err != nil {

		err1 := conn.SetWriteDeadline(time.Time{}) // убираем таймаут для будущих операций
		if err1 != nil {
			return err1
		}

		return err
	}

	err = conn.SetWriteDeadline(time.Time{}) // убираем таймаут для будущих операций
	if err != nil {
		return err
	}

	return nil
}

func write(conn *net.UDPConn, command *command) error {
	b := acquireByteBuffer()
	defer releaseByteBuffer(b)

	bufSize, err := command.marshal(b.buf)
	if err != nil {
		return err
	}

	// say ("Send", len(msg), "bytes:", command, "from", conn.LocalAddr(), "to", conn.RemoteAddr(), "via", conn)
	n, err := conn.Write(b.buf[0:bufSize])
	if err != nil {
		return err
	}
	if n != bufSize { // ??? Такое бывает когда-нибудь?
		return errorln("Partial message send to", conn.RemoteAddr())
	}

	return nil
}

func readWithTimeout(conn *net.UDPConn, timeout time.Duration) (*response, error) {
	err := conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, err
	}

	resp, err := read(conn)
	if err != nil {

		err1 := conn.SetReadDeadline(time.Time{}) // убираем таймаут для будущих операций
		if err1 != nil {
			return nil, err1
		}

		return nil, err
	}

	err = conn.SetReadDeadline(time.Time{}) // убираем таймаут для будущих операций
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func read(conn *net.UDPConn) (*response, error) {

	b := acquireByteBuffer()
	defer releaseByteBuffer(b)

	n, err := conn.Read(b.buf)
	// say ("Received", n, "bytes from", conn.RemoteAddr())
	if err != nil {
		return nil, err
	}

	response := acquireResponse()
	err = response.unmarshal(b.buf[:n])
	if err != nil {
		releaseResponse(response)
		return nil, err
	}

	// say ("Received", n, "bytes", response, "from", conn.RemoteAddr(), "to", conn.LocalAddr(), "via", conn)
	return response, nil
}

func (response *response) unmarshal(buf []byte) error {

	if len(buf) > 508 || len(buf) < 32 {
		return errWrongResponse //errorln("wrong data size", len(buf))
	}

	version := buf[0] // 0 байт - версия
	if version != 1 {
		return errWrongResponse //errorln("unsupported protocol version", version)
	}

	bufSize := int(binary.LittleEndian.Uint16(buf[1:])) // 1 и 2 байты - размер пакета

	if bufSize != len(buf) {
		return errWrongResponse //errorln("wrong packet size", bufSize, len(buf), code2string(buf[3]))
	}

	response.code = buf[3] //  3 байт - команда: CONNECT, OPTIONS, LOCK , UNLOCK и т.д.

	// 4, 5, 6 и 7 байты пока не используются. можно в будущем сюда писать приоритет и чексумму, например
	checksum := binary.LittleEndian.Uint32(buf[4:])

	if checksum != 0 {
		return errWrongResponse //errorln("wrong packet checsum", checksum, "in", fmt.Sprint(response.code))
	}

	switch response.code {

	case OPTIONS:
		if bufSize != 508 {
			return errWrongResponse //errorln("wrong packet size", bufSize, "in OPTIONS")
		}
		response.id.nodeID = binary.LittleEndian.Uint64(buf[8:])
		response.id.connectionID = binary.LittleEndian.Uint64(buf[16:])
		response.id.requestID = binary.LittleEndian.Uint64(buf[24:])

		nodesPos := 32
		nodesNum := int(buf[nodesPos])
		if nodesNum < 1 || nodesNum > 7 { // максимум 7 нод. ??? надо ли больше?
			return errWrongResponse //errorln("wrong number of nodes", nodesNum, "in OPTIONS")
		}

		nodesPos++
		for i := nodesNum; i > 0; i-- {

			//проверяем, что nodesPos не выходит за границы buf
			if (nodesPos + 8 + 1) >= bufSize {
				return errWrongResponse //errorln("wrong nodes position", nodesPos, "in OPTIONS")
			}
			nodeID := binary.LittleEndian.Uint64(buf[nodesPos:])
			nodesPos += 8

			nodeStringSize := int(buf[nodesPos])
			nodesPos++

			//проверяем, что nodesPos не выходит за границы buf
			if (nodesPos + nodeStringSize) > bufSize {
				return errWrongResponse //errorln("wrong nodes string size: from", nodesPos, "to", (nodesPos + nodeStringSize), "in OPTIONS")
			}

			nodeString := string(buf[nodesPos : nodesPos+nodeStringSize])
			nodesPos += nodeStringSize

			response.nodes[nodeID] = nodeString
		}

	case OK, TIMEOUT, BUSY:
		if bufSize != 32 {
			return errWrongResponse //errorln("wrong packet size", bufSize, "in", code2string(response.code))
		}
		response.id.nodeID = binary.LittleEndian.Uint64(buf[8:])
		response.id.connectionID = binary.LittleEndian.Uint64(buf[16:])
		response.id.requestID = binary.LittleEndian.Uint64(buf[24:])

	case REDIRECT:
		if bufSize != 40 {
			return errWrongResponse //errorln("wrong packet size", bufSize, "in REDIRECT")
		}
		response.id.nodeID = binary.LittleEndian.Uint64(buf[8:])
		response.id.connectionID = binary.LittleEndian.Uint64(buf[16:])
		response.id.requestID = binary.LittleEndian.Uint64(buf[24:])
		response.nodeID = binary.LittleEndian.Uint64(buf[32:])

	case ERROR:
		if bufSize < 33 {
			return errWrongResponse //errorln("wrong packet size", bufSize, "in ERROR")
		}
		response.id.nodeID = binary.LittleEndian.Uint64(buf[8:])
		response.id.connectionID = binary.LittleEndian.Uint64(buf[16:])
		response.id.requestID = binary.LittleEndian.Uint64(buf[24:])

		descStringSize := int(buf[32]) // 32 байт - размер адреса ноды
		if descStringSize+33 != bufSize {
			return errWrongResponse //errorln("wrong description size", descStringSize, bufSize, "in ERROR")
		}

		response.description = string(buf[33 : 33+descStringSize])

	case PONG:
		if bufSize != 508 {
			return errWrongResponse //errorln("wrong packet size", bufSize, "in PONG")
		}
		response.id.nodeID = binary.LittleEndian.Uint64(buf[8:])
		response.id.connectionID = binary.LittleEndian.Uint64(buf[16:])
		response.id.requestID = binary.LittleEndian.Uint64(buf[24:])

	default:
		return errWrongResponse //errorln("wrong command code:", fmt.Sprint(response.code))
	}

	return nil
}

const defaultByteBufferSize = 508

var byteBufferPool sync.Pool

func acquireByteBuffer() *byteBuffer {
	v := byteBufferPool.Get()
	if v == nil {
		return &byteBuffer{
			buf: make([]byte, defaultByteBufferSize),
		}
	}
	return v.(*byteBuffer)
}

func releaseByteBuffer(b *byteBuffer) {
	b.buf = b.buf[0:defaultByteBufferSize]
	byteBufferPool.Put(b)
}

type byteBuffer struct {
	buf []byte
}

func (command *command) marshal(buf []byte) (int, error) {

	bufSize := defaultByteBufferSize

	buf[0] = 1 // version:=1
	buf[3] = command.code

	switch command.code {
	case CONNECT:
		// version:=1
		// size:= 508 (1*256+252)
		// command := CONNECT
		// размер пакета 508 байт должен проходить и проходить без фрагметации на любой хост. см. RFC791

	case LOCK, UNLOCK:
		// version:=1
		// size:= расчитываем
		// command := LOCK

		binary.LittleEndian.PutUint64(buf[8:], command.id.nodeID)
		binary.LittleEndian.PutUint64(buf[16:], command.id.connectionID)
		binary.LittleEndian.PutUint64(buf[24:], command.id.requestID)

		binary.LittleEndian.PutUint64(buf[32:], uint64(command.ttl))
		binary.LittleEndian.PutUint64(buf[40:], uint64(command.timeout))

		commandKey := command.key
		commandKeyLen := len(commandKey)
		if commandKeyLen > 255 {
			return 0, ErrLongKey
		}

		buf[48] = byte(commandKeyLen)

		copy(buf[49:], commandKey)

		bufSize = commandKeyLen + 49

	case PING:
		// version:=1
		// size:= 508
		// command := PING

		binary.LittleEndian.PutUint64(buf[8:], command.id.nodeID)
		binary.LittleEndian.PutUint64(buf[16:], command.id.connectionID)
		binary.LittleEndian.PutUint64(buf[24:], command.id.requestID)

	default:
		return 0, errorln("wrong command ", command)
	}

	// выставляем длину пакета
	binary.LittleEndian.PutUint16(buf[1:], uint16(bufSize))

	// заполняем нулями место под чексумму
	binary.LittleEndian.PutUint32(buf[4:], 0)

	return bufSize, nil

}

func errorln(a ...interface{}) error {
	return errors.New(fmt.Sprintln(a))
}

func acquireCommand() *command {
	c := commandPool.Get()
	if c == nil {
		timer := time.NewTimer(time.Hour) // ??? как иначе создать таймер с каналом C != nil?
		timer.Stop()
		return &command{
			respChan:    make(chan error),
			sendChan:    make(chan *node, 2),
			processChan: make(chan *response),
			timer:       timer,
			retries:     0,
		}
	}
	return c.(*command)
}

func releaseCommand(c *command) {
	//atomic.StoreInt32(&c.retries, 0)
	c.retries = 0
	commandPool.Put(c)
}

var commandPool sync.Pool

func acquireResponse() *response {
	c := responsePool.Get()
	if c == nil {
		return &response{
			nodes: make(map[uint64]string),
		}
	}
	return c.(*response)
}

func releaseResponse(c *response) {
	responsePool.Put(c)
}

var responsePool sync.Pool

func (command *command) isEnoughRetries() bool {
	//return atomic.AddInt32(&command.retries, 1) >= command.netmutex.retries
	command.retries++
	return command.retries >= command.netmutex.retries
}

func (netmutex *NetMutex) runCommand(key string, commandID commandID, ttl time.Duration, commandCode byte, timeout time.Duration) error {

	command := acquireCommand()
	defer releaseCommand(command)

	command.id = commandID
	command.code = commandCode
	command.key = key
	command.ttl = ttl
	command.timeout = timeout
	command.netmutex = netmutex

	netmutex.workingCommands.add(command)

	go command.run()
	command.sendChan <- nil

	return <-command.respChan
}

func (command *command) run() {
	for {
		select {
		case <-command.netmutex.done:
			return

		case node := <-command.sendChan:
			command.send(node)

		case <-command.timer.C:
			if command.onTimeout() {
				return
			}

		case resp := <-command.processChan:
			if command.process(resp) {
				return
			}
		}
	}
}

func (command *command) send(node *node) {

	var err error

	for {
		if node == nil {
			node, err = command.netmutex.node()
			if err != nil { // некуда отправлять команду, поэтому сразу возвращаем ошибку

				command.netmutex.workingCommands.delete(command.id)
				command.respChan <- err
				return
			}
		}

		command.currentNode = node

		command.timer.Reset(node.timeout())

		err = write(node.conn, command)
		if err == nil { // если нет ощибки, то выходим из функции и ждём прихода ответа или срабатывания таймера
			return
		}

		command.timer.Stop()

		// если ошибка в том, что длина ключа слишком велика, то помечать ноду плохой не нужно
		if err != ErrLongKey {
			command.currentNode.fail()
		}

		if command.isEnoughRetries() {
			command.netmutex.workingCommands.delete(command.id)
			command.respChan <- ErrTooMuchRetries
			return
		}

		node = nil // чтобы выбрать другую ноду
	}
}

// функция, вызывается, когда истёт таймаут на приход ответа от ноды
func (command *command) onTimeout() bool {
	command.currentNode.fail()

	if command.isEnoughRetries() {
		command.netmutex.workingCommands.delete(command.id)
		command.respChan <- ErrTooMuchRetries
	} else {
		command.sendChan <- nil
		return false
	}
	return true
}

func (command *command) process(resp *response) bool {
	command.timer.Stop()

	switch resp.code {
	case OK:
		command.currentNode.ok()
		command.netmutex.workingCommands.delete(command.id)
		command.respChan <- nil

	case REDIRECT:
		command.currentNode.ok()
		if command.isEnoughRetries() {
			command.netmutex.workingCommands.delete(command.id)
			command.respChan <- ErrTooMuchRetries
		} else {
			command.sendChan <- command.netmutex.nodeByID(resp.nodeID)
			return false
		}

	case TIMEOUT:
		command.currentNode.ok()
		command.netmutex.workingCommands.delete(command.id)
		command.respChan <- ErrTimeout

	case BUSY:
		command.currentNode.fail()

		if command.isEnoughRetries() {
			command.netmutex.workingCommands.delete(command.id)
			command.respChan <- ErrBusy
		} else { // пробуем другую ноду
			command.sendChan <- nil
			return false
		}

	case ERROR:
		command.currentNode.ok()
		command.netmutex.workingCommands.delete(command.id)
		command.respChan <- errors.New(resp.description)

	default:
		command.respChan <- errWrongResponse
	}

	releaseResponse(resp)

	return true
}

//  горутины (по числу нод) читают ответы из своих соединений и направляют их в канал ответов
func (netmutex *NetMutex) readResponses(node *node) {
	conn := node.conn
	for {
		// выходим из цикла, если клиент закончил свою работу
		select {
		case <-netmutex.done:
			netmutex.doneErr <- conn.Close()
			return
		default:
		}

		// таймаут нужен для того, чтобы можно было корректно закончить работу клиента
		resp, err := readWithTimeout(conn, time.Second)

		// если произошёл таймаут или ошибка временная
		if neterr, ok := err.(*net.OpError); ok {
			if neterr.Timeout() {
				continue
			} else if neterr.Temporary() { // ?? что такое временная ошибка?
				node.fail()
				continue
			}
		}

		if err != nil {
			// пример ошибки: read udp 127.0.0.1:19858->127.0.0.1:3002: read: connection refused
			node.fail()
			time.Sleep(100 * time.Millisecond)

		} else {
			netmutex.responses <- resp
		}
	}
}

// пытается открыть соединение
func (netmutex *NetMutex) repairConn(node *node) {
	for {
		select {
		case <-netmutex.done:
			// выходим из цикла, если клиент закончил свою работу
			return

		default:
		}

		conn, err := openConn(node.addr, netmutex.readBufferSize, netmutex.writeBufferSize)

		if err != nil {
			node.fail()
			time.Sleep(time.Minute)
			continue
		}
		node.conn = conn

		go netmutex.readResponses(node)
		return
	}
}

// горутина читает канал ответов
func (netmutex *NetMutex) run() {
	for {
		select {
		case <-netmutex.done:
			return
		case resp := <-netmutex.responses:
			if resp.code == OPTIONS {
				// переконфигурация: новый список нод, новый уникальный commandID
				// ToDo написать переконфигурацию
				releaseResponse(resp)
				continue
			}

			command, ok := netmutex.workingCommands.get(resp.id)
			// если команда не нашлась по ID, то ждём следующую
			if !ok {
				releaseResponse(resp)
				continue
			}
			command.processChan <- resp
		}
	}
}

type workingCommands struct {
	sync.RWMutex
	m map[commandID]*command
}

func (wc *workingCommands) add(command *command) {
	wc.Lock()
	wc.m[command.id] = command
	wc.Unlock()
}

func (wc *workingCommands) get(commandID commandID) (*command, bool) {
	wc.RLock()
	defer wc.RUnlock()

	command, ok := wc.m[commandID]
	return command, ok
}

func (wc *workingCommands) delete(commandID commandID) {
	wc.Lock()
	defer wc.Unlock()

	delete(wc.m, commandID)
}

const (
	DefaultTTL             = 8766 * time.Hour // 1 year
	DefaultTimeout         = time.Hour
	DefaultRetries         = int32(7)
	DefaultReadBufferSize  = 0 // 0 - means OS default
	DefaultWriteBufferSize = 0 // 0 - means OS default
)

// соединяется к первой ответившей ноде из списка,
// получает с неё актуальный список нод,
// задаёт параметры соединений, дефолтные ttl и timeout для будущих запросов
func Open(addrs []string, options *Options) (*NetMutex, error) {

	netmutex := &NetMutex{
		ttl:             DefaultTTL,
		timeout:         DefaultTimeout,
		retries:         DefaultRetries,
		readBufferSize:  DefaultReadBufferSize,
		writeBufferSize: DefaultWriteBufferSize,
		done:            make(chan struct{}),
		doneErr:         make(chan error),
		responses:       make(chan *response),
		workingCommands: &workingCommands{
			m: make(map[commandID]*command),
		},
	}

	if options != nil {
		if options.TTL > 0 {
			netmutex.ttl = options.TTL
		}
		if options.Timeout > 0 {
			netmutex.timeout = options.Timeout
		}
		if options.Retries > 0 {
			netmutex.retries = options.Retries
		}
		if options.ReadBufferSize > 0 {
			netmutex.readBufferSize = options.ReadBufferSize
		}
		if options.WriteBufferSize > 0 {
			netmutex.writeBufferSize = options.WriteBufferSize
		}
	}

	// обходим все сервера из списка
	for _, addr := range addrs {

		options, err := netmutex.connectOptions(addr)
		if err != nil {
			continue
		}

		netmutex.nextCommandID = options.id

		remoteNodes := make(map[uint64]*node)

		// пробуем соединиться с нодами из полученного в ответе списка,
		// отправить им PING, получить PONG, тем самым проверив прохождение пакетов
		// а у не прошедших проверку нод увеличить Fails
		for nodeID, nodeAddr := range options.nodes {

			remoteNodes[nodeID] = &node{
				id:   nodeID,
				addr: nodeAddr,
				mtu:  508, // ToDo брать из пинг-понга
				rtt:  1,   // ToDo брать из пинг-понга
			}

			conn, err := openConn(remoteNodes[nodeID].addr, netmutex.readBufferSize, netmutex.writeBufferSize)
			if err != nil {
				remoteNodes[nodeID].fail()
				continue
			}
			remoteNodes[nodeID].conn = conn

			err = netmutex.pingPong(remoteNodes[nodeID])
			if err != nil {
				remoteNodes[nodeID].fail()
			}
		}

		netmutex.nodes = &nodes{
			m: remoteNodes,
		}

		for _, node := range netmutex.nodes.m {
			if node.conn != nil {
				go netmutex.readResponses(node)
			} else {
				go netmutex.repairConn(node)
			}
		}
		go netmutex.run()

		return netmutex, nil
	}

	return nil, ErrNoNodes
}

func (netmutex *NetMutex) connectOptions(addr string) (*response, error) {
	conn, err := openConn(addr, 0, 0)
	if err != nil {
		return nil, err
	}

	req := &command{
		code: CONNECT,
	}

	err = writeWithTimeout(conn, req, time.Second)
	if err != nil {

		err1 := conn.Close()
		if err1 != nil {
			return nil, err1
		}

		return nil, err
	}

	resp, err := readWithTimeout(conn, time.Second)
	if err != nil || resp.code != OPTIONS {

		err1 := conn.Close()
		if err1 != nil {
			return nil, err1
		}

		return nil, err
	}

	err = conn.Close()
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (netmutex *NetMutex) pingPong(node *node) error {
	pingCommand := &command{
		code: PING,
		id:   netmutex.commandID(),
	}

	err := writeWithTimeout(node.conn, pingCommand, time.Second)
	if err != nil {
		return err
	}

	pongCommand, err := readWithTimeout(node.conn, time.Second)
	if err != nil || pongCommand.code != PONG || pongCommand.id != pingCommand.id {
		return err
	}

	return nil
}

func openConn(addr string, readBufferSize int, writeBufferSize int) (*net.UDPConn, error) {
	nodeAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, nodeAddr)
	if err != nil {
		return nil, err
	}

	if writeBufferSize > 0 {
		err = conn.SetReadBuffer(readBufferSize)
		if err != nil {

			err1 := conn.Close()
			if err1 != nil {
				return nil, err1
			}

			return nil, err
		}
	}

	if writeBufferSize > 0 {
		err = conn.SetWriteBuffer(writeBufferSize)
		if err != nil {

			err1 := conn.Close()
			if err1 != nil {
				return nil, err1
			}

			return nil, err
		}
	}

	return conn, nil
}

func (netmutex *NetMutex) Close() error {
	close(netmutex.done)
	return <-netmutex.doneErr
}
