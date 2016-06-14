package client

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type responce struct {
	id          commandId         // уникальный номер команды
	nodes       map[uint64]string // список нод при OPTIONS
	node_id     uint64            // адрес ноды при REDIRECT
	description string            // описание ошибки при ERROR
	code        byte              // код команды. Должно быть в хвосте структуры, ибо портит выравнивание всех остальных полей
}

type command struct {
	id           commandId      // уникальный номер команды
	key          string         // ключ
	ttl          time.Duration  // на сколько лочим ключ
	timeout      time.Duration  // за какое время надо попытаться выполнить команду
	resp_chan    chan error     // канал, в который пишется ответ от ноды
	send_chan    chan *node     // канал, в который пишется нода, на которую надо послать запрос
	process_chan chan *responce // канал, в который пишется ответ от ноды, который надо обработать
	retries      int32          // количество запросов к нодам, прежде чем вернуть ошибку
	client       *Client        // ссылка на клиента для чтения списка нод и retries
	current_node *node          // нода, которая обработывает текущего запрос
	timer        *time.Timer    // таймер текущего запроса
	code         byte           // код команды. Должно быть в хвосте структуры, ибо портит выравнивание всех остальных полей
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

func code2string(code byte) string {
	switch code {
	case CONNECT:
		return "CONNECT"
	case OPTIONS:
		return "OPTIONS"
	case LOCK:
		return "LOCK"
	case UNLOCK:
		return "UNLOCK"
	case OK:
		return "OK"
	case REDIRECT:
		return "REDIRECT"
	case TIMEOUT:
		return "TIMEOUT"
	case BUSY:
		return "BUSY"
	case ERROR:
		return "ERROR"
	case PING:
		return "PING"
	case PONG:
		return "PONG"
	default:
		fmt.Fprintln(os.Stderr, "Wrong code:", fmt.Sprint(code))
		return ""
	}
}

type commandId struct {
	node_id       uint64
	connection_id uint64
	request_id    uint64
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

type Client struct {
	next_command_id   commandId     // должна быть первым полем в структуре, иначе может быть неверное выравнивание и atomic перестанет работать
	ttl               time.Duration // значение по умолчанию для Lock(), Unlock()
	timeout           time.Duration // значение по умолчанию для Timeout
	retries           int32
	read_buffer_size  int
	write_buffer_size int
	done              chan struct{}
	responces         chan *responce
	nodes             *nodes
	working_commands  *working_commands
}

func (client *Client) command_id() commandId {
	return commandId{
		node_id:       client.next_command_id.node_id,
		connection_id: client.next_command_id.connection_id,
		request_id:    atomic.AddUint64(&client.next_command_id.request_id, 1),
	}
}

type by_fails []node

func (a by_fails) Len() int {
	return len(a)
}
func (a by_fails) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}
func (a by_fails) Less(i, j int) bool {
	//	if a[i].fails == a[j].fails {
	//		return a[i].id < a[j].id
	//	}
	return a[i].fails < a[j].fails
}

func (client *Client) node() (*node, error) {
	client.nodes.Lock()
	defer client.nodes.Unlock()

	if client.nodes.current != nil {
		if atomic.LoadUint32(&client.nodes.current.fails) == 0 {
			return client.nodes.current, nil
		}
	}

	// копируем в массив для сортировки
	var client_nodes by_fails
	for _, n := range client.nodes.m {
		// пропускаем несоединившиеся ноды и текущую ноду
		if n.conn != nil && client.nodes.current != n {
			client_nodes = append(client_nodes, node{ // чтобы избежать при сортировке atomic.Load()
				id:    n.id,
				fails: atomic.LoadUint32(&n.fails),
				mtu:   n.mtu,
				rtt:   n.rtt,
			})
		}
	}

	sort.Sort(client_nodes)

	//	for _, node := range client_nodes {
	//		say(node.id,node.fails)
	//	}

	for _, node := range client_nodes {
		client.nodes.current = client.nodes.m[node.id]
		return client.nodes.current, nil
	}
	return nil, errors.New("No working nodes.")
}

func (client *Client) node_by_id(node_id uint64) *node {
	client.nodes.Lock()
	defer client.nodes.Unlock()

	if node, ok := client.nodes.m[node_id]; ok {
		return node
	}

	return nil
}

// заблокировать, если не было заблокировано ранее
func (client *Client) LockIfUnlocked(key string) (*Lock, error) {
	return nil, errors.New("LockIfUnlocked() has not yet implemented.")
}

// заблокировать все или ниодного.
func (client *Client) LockEach(keys []string) ([]*Lock, error) {
	return nil, errors.New("LockEach() has not yet implemented.")
}

// заблокировать всё, что получится.
func (client *Client) LockAny(keys []string) ([]*Lock, error) {
	return nil, errors.New("LockAny() has not yet implemented.")
}

// заблокировать на чтение
func (client *Client) RLock(key string) (*Lock, error) {
	return client.Lock(key) // ToDo переделать
}

// за время timeout снять ранее установленную блокировку на чтение
func (lock *Lock) RUnlock() error {
	return lock.Unlock() // ToDo переделать
}

// снять все блокировки
func (client *Client) UnlockAll() error {
	return errors.New("UnlockAll() has not yet implemented.")
}

// за время timeout установить блокировку ключа key на время ttl
// Lock(key string)
func (client *Client) Lock(key string) (*Lock, error) {
	timeout := client.timeout
	ttl := client.ttl
	command_id := client.command_id()

	err := client.run_command(key, command_id, ttl, LOCK, timeout)
	if err == nil {
		return &Lock{
				key:        key,
				client:     client,
				command_id: command_id,
				timeout:    timeout,
			},
			nil
	}
	return nil, err
}

type Lock struct {
	key        string
	client     *Client
	command_id commandId
	timeout    time.Duration
}

// обновить таймаут у существующей блокировки
func (lock *Lock) Update(ttl time.Duration, timeout time.Duration) error {
	return errors.New("Update() has not yet implemented.")
}

// за время timeout снять ранее установленную блокировку
func (lock *Lock) Unlock() error {
	return lock.client.run_command(lock.key, lock.command_id, 0, UNLOCK, lock.timeout)
}

func write_with_timeout(conn *net.UDPConn, command *command, timeout time.Duration) error {
	conn.SetWriteDeadline(time.Now().Add(timeout))
	defer conn.SetWriteDeadline(time.Time{}) // убираем таймаут для будущих операций
	return write(conn, command)
}

func write(conn *net.UDPConn, command *command) error {
	buf := acquire_byte_buffer()
	defer release_byte_buffer(buf)

	buf_size, err := command.marshal(buf)
	if err != nil {
		return err
	}
	buf = buf[0:buf_size]

	// say ("Send", len(msg), "bytes:", command, "from", conn.LocalAddr(), "to", conn.RemoteAddr(), "via", conn)
	n, err := conn.Write(buf[0:buf_size])
	if err != nil {
		return err
	}
	if n != buf_size { // ??? Такое бывает когда-нибудь?
		return errorln("Partial message send to", conn.RemoteAddr())
	}

	return nil
}

func read_with_timeout(conn *net.UDPConn, timeout time.Duration) (*responce, error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	defer conn.SetReadDeadline(time.Time{}) // убираем таймаут для будущих операций
	return read(conn)
}

func read(conn *net.UDPConn) (*responce, error) {

	buf := acquire_byte_buffer()
	defer release_byte_buffer(buf)

	n, err := conn.Read(buf)
	// say ("Received", n, "bytes from", conn.RemoteAddr())
	if err != nil {
		return nil, err
	}

	responce := acquire_responce()
	err = responce.unmarshal(buf[:n])
	if err != nil {
		release_responce(responce)
		return nil, err
	}

	// say ("Received", n, "bytes", responce, "from", conn.RemoteAddr(), "to", conn.LocalAddr(), "via", conn)
	return responce, nil
}

func (responce *responce) unmarshal(buf []byte) error {

	if len(buf) > 508 || len(buf) < 32 {
		return errorln("wrong data size", len(buf))
	}

	version := buf[0] // 0 байт - версия
	if version != 1 {
		return errorln("unsupported protocol version", version)
	}

	buf_size := int(binary.LittleEndian.Uint16(buf[1:])) // 1 и 2 байты - размер пакета

	if buf_size != len(buf) {
		return errorln("wrong packet size", buf_size, len(buf), code2string(buf[3]))
	}

	responce.code = buf[3] //  3 байт - команда: CONNECT, OPTIONS, LOCK , UNLOCK и т.д.

	// 4, 5, 6 и 7 байты пока не используются. можно в будущем сюда писать приоритет и чексумму, например
	checksum := binary.LittleEndian.Uint32(buf[4:])

	if checksum != 0 {
		return errorln("wrong packet checsum", checksum, "in", fmt.Sprint(responce.code))
	}

	switch responce.code {

	case OPTIONS:
		if buf_size != 508 {
			return errorln("wrong packet size", buf_size, "in OPTIONS")
		}
		responce.id.node_id = binary.LittleEndian.Uint64(buf[8:])
		responce.id.connection_id = binary.LittleEndian.Uint64(buf[16:])
		responce.id.request_id = binary.LittleEndian.Uint64(buf[24:])

		nodes_pos := 32
		nodes_num := int(buf[nodes_pos])
		if nodes_num < 1 || nodes_num > 7 { // максимум 7 нод. ??? надо ли больше?
			return errorln("wrong number of nodes", nodes_num, "in OPTIONS")
		}

		nodes_pos++
		for i := nodes_num; i > 0; i-- {

			//проверяем, что nodes_pos не выходит за границы buf
			if (nodes_pos + 8 + 1) >= buf_size {
				return errorln("wrong nodes position", nodes_pos, "in OPTIONS")
			}
			node_id := binary.LittleEndian.Uint64(buf[nodes_pos:])
			nodes_pos += 8

			node_string_size := int(buf[nodes_pos])
			nodes_pos++

			//проверяем, что nodes_pos не выходит за границы buf
			if (nodes_pos + node_string_size) > buf_size {
				return errorln("wrong nodes string size: from", nodes_pos, "to", (nodes_pos + node_string_size), "in OPTIONS")
			}

			node_string := string(buf[nodes_pos : nodes_pos+node_string_size])
			nodes_pos += node_string_size

			responce.nodes[node_id] = node_string
		}

	case OK, TIMEOUT, BUSY:
		if buf_size != 32 {
			return errorln("wrong packet size", buf_size, "in", code2string(responce.code))
		}
		responce.id.node_id = binary.LittleEndian.Uint64(buf[8:])
		responce.id.connection_id = binary.LittleEndian.Uint64(buf[16:])
		responce.id.request_id = binary.LittleEndian.Uint64(buf[24:])

	case REDIRECT:
		if buf_size != 40 {
			return errorln("wrong packet size", buf_size, "in REDIRECT")
		}
		responce.id.node_id = binary.LittleEndian.Uint64(buf[8:])
		responce.id.connection_id = binary.LittleEndian.Uint64(buf[16:])
		responce.id.request_id = binary.LittleEndian.Uint64(buf[24:])
		responce.node_id = binary.LittleEndian.Uint64(buf[32:])

	case ERROR:
		if buf_size < 33 {
			return errorln("wrong packet size", buf_size, "in ERROR")
		}
		responce.id.node_id = binary.LittleEndian.Uint64(buf[8:])
		responce.id.connection_id = binary.LittleEndian.Uint64(buf[16:])
		responce.id.request_id = binary.LittleEndian.Uint64(buf[24:])

		desc_string_size := int(buf[32]) // 32 байт - размер адреса ноды
		if desc_string_size+33 != buf_size {
			return errorln("wrong description size", desc_string_size, buf_size, "in ERROR")
		}

		responce.description = string(buf[33 : 33+desc_string_size])

	case PONG:
		if buf_size != 508 {
			return errorln("wrong packet size", buf_size, "in PONG")
		}
		responce.id.node_id = binary.LittleEndian.Uint64(buf[8:])
		responce.id.connection_id = binary.LittleEndian.Uint64(buf[16:])
		responce.id.request_id = binary.LittleEndian.Uint64(buf[24:])

	default:
		return errorln("wrong command code:", fmt.Sprint(responce.code))
	}

	return nil
}

const default_byte_buffer_size = 508

func acquire_byte_buffer() []byte {
	v := byte_buffer_pool.Get()
	if v == nil {
		return make([]byte, default_byte_buffer_size)
	}
	return v.([]byte)
}

func release_byte_buffer(b []byte) {
	byte_buffer_pool.Put(b[0:default_byte_buffer_size])
}

var byte_buffer_pool sync.Pool

func (command *command) marshal(buf []byte) (int, error) {

	buf_size := default_byte_buffer_size

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

		binary.LittleEndian.PutUint64(buf[8:], command.id.node_id)
		binary.LittleEndian.PutUint64(buf[16:], command.id.connection_id)
		binary.LittleEndian.PutUint64(buf[24:], command.id.request_id)

		binary.LittleEndian.PutUint64(buf[32:], uint64(command.ttl))
		binary.LittleEndian.PutUint64(buf[40:], uint64(command.timeout))

		command_key := command.key
		command_key_len := len(command_key)
		if command_key_len > 255 {
			return 0, errorln("key too long", command_key_len)
		}

		buf[48] = byte(command_key_len)

		copy(buf[49:], command_key)

		buf_size = command_key_len + 49

	case PING:
		// version:=1
		// size:= 508
		// command := PING

		binary.LittleEndian.PutUint64(buf[8:], command.id.node_id)
		binary.LittleEndian.PutUint64(buf[16:], command.id.connection_id)
		binary.LittleEndian.PutUint64(buf[24:], command.id.request_id)

	default:
		return 0, errorln("wrong command ", command)
	}

	// выставляем длину пакета
	binary.LittleEndian.PutUint16(buf[1:], uint16(buf_size))

	// заполняем нулями место под чексумму
	binary.LittleEndian.PutUint32(buf[4:], 0)

	return buf_size, nil

}

func errorln(a ...interface{}) error {
	return errors.New(fmt.Sprintln(a))
}

func acquire_command() *command {
	c := command_pool.Get()
	if c == nil {
		timer := time.NewTimer(time.Hour) // ??? как иначе создать таймер с каналом C != nil?
		timer.Stop()
		return &command{
			resp_chan:    make(chan error),
			send_chan:    make(chan *node, 2),
			process_chan: make(chan *responce),
			timer:        timer,
			retries:      0,
		}
	}
	return c.(*command)
}

func release_command(c *command) {
	//atomic.StoreInt32(&c.retries, 0)
	c.retries = 0
	command_pool.Put(c)
}

var command_pool sync.Pool

func acquire_responce() *responce {
	c := responce_pool.Get()
	if c == nil {
		return &responce{
			nodes: make(map[uint64]string),
		}
	}
	return c.(*responce)
}

func release_responce(c *responce) {
	responce_pool.Put(c)
}

var responce_pool sync.Pool

var (
	ErrorTimeout   = errors.New("Timeout exceeded.")
	ErrorBusy      = errors.New("Servers busy.")
	ErrorWrongCode = errors.New("Wrong responce code.")
)

func (command *command) is_enough_retries() bool {
	//return atomic.AddInt32(&command.retries, 1) >= command.client.retries
	command.retries++
	return command.retries >= command.client.retries
}

func (client *Client) run_command(key string, command_id commandId, ttl time.Duration, command_code byte, timeout time.Duration) error {

	command := acquire_command()
	defer release_command(command)

	command.id = command_id
	command.code = command_code
	command.key = key
	command.ttl = ttl
	command.timeout = timeout
	command.client = client

	client.working_commands.add(command)

	go command.run()
	command.send_chan <- nil

	return <-command.resp_chan
}

func (command *command) run() {
	for {
		select {
		case <-command.client.done:
			return

		case node := <-command.send_chan:
			command.send(node)

		case <-command.timer.C:
			if command.on_timeout() {
				return
			}

		case resp := <-command.process_chan:
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
			node, err = command.client.node()
			if err != nil { // некуда отправлять команду, поэтому сразу возвращаем ошибку

				command.client.working_commands.delete(command.id)
				command.resp_chan <- err
				return
			}
		}

		command.current_node = node

		command.timer.Reset(node.timeout())

		err = write(node.conn, command)
		if err == nil { // если нет ощибки, то выходим из функции и ждём прихода ответа или срабатывания таймера
			return
		}

		command.timer.Stop()
		command.current_node.fail()

		if command.is_enough_retries() {

			command.client.working_commands.delete(command.id)
			command.resp_chan <- errorln("Too much retries. Last error:", err)
			return
		}
		node = nil // чтобы выбрать другую ноду
	}
}

// функция, вызывается, когда истёт таймаут на приход ответа от ноды
func (command *command) on_timeout() bool {
	command.current_node.fail()

	if command.is_enough_retries() {
		command.client.working_commands.delete(command.id)
		command.resp_chan <- errors.New("Too much retries. Last error: last request timeouted.")
	} else {
		command.send_chan <- nil
		return false
	}
	return true
}

func (command *command) process(resp *responce) bool {
	command.timer.Stop()

	switch resp.code {
	case OK:
		command.current_node.ok()
		command.client.working_commands.delete(command.id)
		command.resp_chan <- nil

	case REDIRECT:
		command.current_node.ok()
		if command.is_enough_retries() {
			command.client.working_commands.delete(command.id)
			command.resp_chan <- errors.New("Too much retries.")
		} else {
			command.send_chan <- command.client.node_by_id(resp.node_id)
			return false
		}

	case TIMEOUT:
		command.current_node.ok()
		command.client.working_commands.delete(command.id)
		command.resp_chan <- errors.New("Timeout exceeded.")

	case BUSY:
		command.current_node.fail()

		if command.is_enough_retries() {
			command.client.working_commands.delete(command.id)
			command.resp_chan <- errors.New("Too much retries. Last error: Server busy. Load too high.")
		} else { // пробуем другую ноду
			command.send_chan <- nil
			return false
		}

	case ERROR:
		command.current_node.ok()
		command.client.working_commands.delete(command.id)
		command.resp_chan <- errors.New(resp.description)

	default:
		command.resp_chan <- errors.New("Wrong command type.")
	}

	release_responce(resp)

	return true
}

//  горутины (по числу нод) читают ответы из своих соединений и направляют их в канал ответов
func (client *Client) read_responces(node *node) {
	conn := node.conn
	for {
		// выходим из цикла, если клиент закончил свою работу
		select {
		case <-client.done:
			conn.Close()
			return
		default:
		}

		// таймаут нужен для того, чтобы можно было корректно закончить работу клиента
		resp, err := read_with_timeout(conn, time.Second)

		// если произошёл таймаут или ошибка временная
		if neterr, ok := err.(*net.OpError); ok {
			if neterr.Timeout() {
				continue
			} else if neterr.Temporary() {
				continue
			}
		}

		if err != nil {
			// не ясно из-за чего сюда можем попасть и как быть?
			fmt.Fprintln(os.Stderr, err)
			time.Sleep(100 * time.Millisecond)

		} else {
			client.responces <- resp
		}
	}
}

// пытается открыть соединение
func (client *Client) repair_conn(node *node) {
	for {
		select {
		case <-client.done:
			// выходим из цикла, если клиент закончил свою работу
			return

		default:
		}

		conn, err := open_conn(node.addr, client.read_buffer_size, client.write_buffer_size)

		if err != nil {
			node.fail()
			time.Sleep(time.Minute)
			continue
		}
		node.conn = conn

		go client.read_responces(node)
		return
	}
}

// горутина читает канал ответов
func (client *Client) run() {
	for {
		select {
		case <-client.done:
			return
		case resp := <-client.responces:
			if resp.code == OPTIONS {
				// переконфигурация: новый список нод, новый уникальный command_id
				// ToDo написать переконфигурацию
				release_responce(resp)
				continue
			}

			command, ok := client.working_commands.get(resp.id)
			// если команда не нашлась по Id, то ждём следующую
			if !ok {
				release_responce(resp)
				continue
			}
			command.process_chan <- resp
		}
	}
}

type working_commands struct {
	sync.Mutex
	m map[commandId]*command
}

func (wc *working_commands) add(command *command) {
	wc.Lock()
	wc.m[command.id] = command
	wc.Unlock()
}

func (wc *working_commands) get(command_id commandId) (*command, bool) {
	wc.Lock()
	defer wc.Unlock()

	command, ok := wc.m[command_id]
	return command, ok
}

func (wc *working_commands) delete(command_id commandId) {
	wc.Lock()
	defer wc.Unlock()

	delete(wc.m, command_id)
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
func Open(addrs []string, options *Options) (*Client, error) {

	client := &Client{
		ttl:               DefaultTTL,
		timeout:           DefaultTimeout,
		retries:           DefaultRetries,
		read_buffer_size:  DefaultReadBufferSize,
		write_buffer_size: DefaultWriteBufferSize,
		done:              make(chan struct{}),
		responces:         make(chan *responce),
		working_commands: &working_commands{
			m: make(map[commandId]*command),
		},
	}

	if options != nil {
		if options.TTL > 0 {
			client.ttl = options.TTL
		}
		if options.Timeout > 0 {
			client.timeout = options.Timeout
		}
		if options.Retries > 0 {
			client.retries = options.Retries
		}
		if options.ReadBufferSize > 0 {
			client.read_buffer_size = options.ReadBufferSize
		}
		if options.WriteBufferSize > 0 {
			client.write_buffer_size = options.WriteBufferSize
		}
	}

	// обходим все сервера из списка
	for _, addr := range addrs {

		options, err := client.connect_options(addr)
		if err != nil {
			continue
		}

		client.next_command_id = options.id

		remote_nodes := make(map[uint64]*node)

		// пробуем соединиться с нодами из полученного в ответе списка,
		// отправить им PING, получить PONG, тем самым проверив прохождение пакетов
		// а у не прошедших проверку нод увеличить Fails
		for node_id, node_addr := range options.nodes {

			remote_nodes[node_id] = &node{
				id:   node_id,
				addr: node_addr,
				mtu:  508, // ToDo брать из пинг-понга
				rtt:  1,   // ToDo брать из пинг-понга
			}

			conn, err := open_conn(remote_nodes[node_id].addr, client.read_buffer_size, client.write_buffer_size)
			if err != nil {
				remote_nodes[node_id].fail()
				continue
			}
			remote_nodes[node_id].conn = conn

			err = client.ping_pong(remote_nodes[node_id])
			if err != nil {
				remote_nodes[node_id].fail()
			}
		}

		client.nodes = &nodes{
			m: remote_nodes,
		}

		for _, node := range client.nodes.m {
			if node.conn != nil {
				go client.read_responces(node)
			} else {
				go client.repair_conn(node)
			}
		}
		go client.run()

		return client, nil
	}

	return nil, errors.New("No nodes to connect")
}

func (client *Client) connect_options(addr string) (*responce, error) {
	conn, err := open_conn(addr, 0, 0)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	req := &command{
		code: CONNECT,
	}

	err = write_with_timeout(conn, req, time.Second)
	if err != nil {
		return nil, err
	}

	resp, err := read_with_timeout(conn, time.Second)
	if err != nil || resp.code != OPTIONS {
		return nil, err
	}

	return resp, nil
}

func (client *Client) ping_pong(node *node) error {
	ping_command := &command{
		code: PING,
		id:   client.command_id(),
	}

	err := write_with_timeout(node.conn, ping_command, time.Second)
	if err != nil {
		return err
	}

	pong_command, err := read_with_timeout(node.conn, time.Second)
	if err != nil || pong_command.code != PONG || pong_command.id != ping_command.id {
		return err
	}

	return nil
}

func open_conn(addr string, read_buffer_size int, write_buffer_size int) (*net.UDPConn, error) {
	node_addr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialUDP("udp", nil, node_addr)
	if err != nil {
		return nil, err
	}

	if write_buffer_size > 0 {
		err = conn.SetReadBuffer(read_buffer_size)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	if write_buffer_size > 0 {
		err = conn.SetWriteBuffer(write_buffer_size)
		if err != nil {
			conn.Close()
			return nil, err
		}
	}

	return conn, nil
}

func (client *Client) Close() {
	client.done <- struct{}{}
}
