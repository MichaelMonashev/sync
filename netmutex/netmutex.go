// Package netmutex implements low-level high performance client library for lock server.
// Очень важно корректно обрабатывать ошибки, которые возвращают функции.
package netmutex

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MichaelMonashev/sync/netmutex/code"
)

// Ограничения на размер передаваемых в команды данных.
const (
	// MaxKeySize - максимальная допустимая длина ключа.
	MaxKeySize = 255

	// MaxIsolationInfo - максимальная допустимая длина информации для изоляции клиента в случае его неработоспособности.
	MaxIsolationInfo = 400
)

// Коды возвращаемых ошибок.
var (
	// ErrDisconnected - cоединение было закрыто ранее.
	ErrDisconnected = errors.New("Client connection had closed.")

	// ErrIsolated - клиент был изолирован. Нужно завершить работу с программы.
	ErrIsolated = errors.New("Client had isolated.")

	// ErrLocked - ключ заблокирован кем-то другим.
	ErrLocked = errors.New("Key locked.")

	// ErrNoServers - не удалось подключиться ни к одному серверу из списка или все они стали недоступны.
	ErrNoServers = errors.New("No working servers.")

	// ErrTooMuchRetries - превышено количество попыток отправить команду на сервер.
	ErrTooMuchRetries = errors.New("Too much retries.")

	// ErrLongKey - ключ длиннее MaxKeySize байт.
	ErrLongKey = errors.New("Key too long.")

	// ErrWrongTTL - TTL меньше нуля.
	ErrWrongTTL = errors.New("Wrong TTL.")

	// ErrLongIsolationInfo - информация для изоляции клиента длиннее MaxIsolationInfo байт.
	ErrLongIsolationInfo = errors.New("Client isolation information too long.")
)

var (
	errWrongResponse = errors.New("Wrong response.")
	errLockIsNil     = errors.New("Try to use nil lock.")
)

type servers struct {
	sync.Mutex
	m       map[uint64]*server
	current *server
}

// Lock - это блокировка.
type Lock struct {
	key       string
	commandID commandID
	timeout   time.Duration
}

// Options задаёт дополнительные параметры соединения.
type Options struct {
	IsolationInfo string // Информация о том, как в случае неработоспособности будет изолироваться клиент.
}

// NetMutex реализует блокировки по сети.
type NetMutex struct {
	nextCommandID   commandID // должна быть первым полем в структуре, иначе может быть неверное выравнивание и atomic перестанет работать
	done            chan struct{}
	servers         *servers
	workingCommands *workingCommands
}

// Open пытается за время timeout соединиться с любым сервером из списка addrs,
// получить с него актуальный список серверов, используя параметры options.
// Если не получается, то повторяет обход retries раз.
// Если получается, то пытается открыть соединение с каждым сервером из полученного списка серверов.
func Open(retries int, timeout time.Duration, addrs []string, options *Options) (*NetMutex, error) {

	nm := &NetMutex{
		done:            make(chan struct{}),
		workingCommands: NewWorkingCommands(),
	}

	isolationInfo := ""
	if options != nil {
		isolationInfo = options.IsolationInfo
	}

	if len(isolationInfo) > MaxIsolationInfo {
		return nil, ErrLongIsolationInfo
	}

	// обходим все сервера из списка, пока не найдём доступный
	for i := 0; i < retries; i++ {
		for _, addr := range addrs {
			resp, err := nm.connect(addr, timeout, isolationInfo)
			if err != nil {
				continue
			}
			nm.nextCommandID = resp.id

			remoteServers := make(map[uint64]*server)

			// пробуем соединиться с серверами из полученного в ответе списка,
			// отправить им PING, получить OK, тем самым проверив прохождение пакетов
			// а у не прошедших проверку серверов увеличить Fails
			for serverID, serverAddr := range resp.servers {

				s := &server{
					id:   serverID,
					addr: serverAddr,
				}

				remoteServers[serverID] = s

				s.conn, err = openConn(s.addr)
				if err != nil {
					s.fail()
					continue
				}

				err = nm.ping(s, timeout)
				if err != nil {
					s.fail()
				}
			}

			nm.servers = &servers{
				m: remoteServers,
			}

			// запускаем горутины, ждущие ответов от каждого сервера
			for _, server := range nm.servers.m {

				if server.conn != nil {
					go nm.readResponses(server)
				} else {
					go nm.repairConn(server)
				}
			}

			return nm, nil
		}
	}

	return nil, ErrNoServers
}

// Lock пытается заблокировать ключ key, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout. Если блокировка удалась, то она записывается в lock.
func (nm *NetMutex) Lock(retries int, timeout time.Duration, lock *Lock, key string, ttl time.Duration) error {

	if len(key) > MaxKeySize {
		return ErrLongKey
	}

	if ttl < 0 {
		return ErrWrongTTL
	}

	id := nm.commandID()

	err := nm.runCommand(key, id, code.LOCK, timeout, ttl, commandID{}, retries)
	if err != nil {
		return err
	}

	lock.key = key
	lock.commandID = id
	lock.timeout = timeout

	return nil

}

// Update пытается у блокировки lock обновить ttl, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout.
// Позволяет продлять время жизни блокировки.
// Подходит для реализации heartbeat-а, позволяющего оптимальнее управлять ttl ключа.
func (nm *NetMutex) Update(retries int, timeout time.Duration, lock *Lock, ttl time.Duration) error {
	if lock == nil {
		return errLockIsNil
	}

	if len(lock.key) > MaxKeySize {
		return ErrLongKey
	}

	if ttl < 0 {
		return ErrWrongTTL
	}

	return nm.runCommand(lock.key, nm.commandID(), code.UPDATE, timeout, ttl, lock.commandID, retries)
}

// Unlock пытается снять блокировку lock, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout.
func (nm *NetMutex) Unlock(retries int, timeout time.Duration, lock *Lock) error {
	if lock == nil {
		return errLockIsNil
	}

	if len(lock.key) > MaxKeySize {
		return ErrLongKey
	}

	return nm.runCommand(lock.key, nm.commandID(), code.UNLOCK, lock.timeout, 0, lock.commandID, retries)
}

// UnlockAll пытается снять все блокировки, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout.
// Использовать с осторожностью!!! Клиенты, у которых блокировки установлены, изолируются!
func (nm *NetMutex) UnlockAll(retries int, timeout time.Duration) error {
	return nm.runCommand("", nm.commandID(), code.UNLOCKALL, timeout, 0, commandID{}, retries)
}

// Close закрывает соединение с сервером блокировок. Нужно только для сервера, чтобы он мог сразу удалить из памяти информацию о соединении, а не по таймауту.
func (nm *NetMutex) Close(retries int, timeout time.Duration) error {
	defer close(nm.done)
	return nm.runCommand("", nm.commandID(), code.DISCONNECT, timeout, 0, commandID{}, retries)
}

func (nm *NetMutex) runCommand(key string, id commandID, code byte, timeout time.Duration, ttl time.Duration, lockID commandID, retries int) error {

	command := getRequest()
	defer putRequest(command)

	command.id = id
	command.code = code
	command.key = key
	command.timeout = timeout
	command.ttl = ttl
	command.lockID = lockID
	command.retries = retries
	command.netmutex = nm

	nm.workingCommands.add(command)
	defer nm.workingCommands.delete(command.id)

	go command.run()

	return <-command.respChan
}

func (nm *NetMutex) touch(s *server) {
	command := getRequest()
	defer putRequest(command)

	command.code = code.TOUCH

	for {
		time.Sleep(10 * time.Minute) // ToDo Вынести в константы

		// выходим из цикла, если клиент закончил свою работу
		select {
		case <-nm.done:
			return
		default:
		}

		command.id = nm.commandID()

		write(s, command) // ответ именно для этого command.id нам не важен, так что не запускаем горутину, ждущую именно этот ответ.
	}
}

//  горутины (по числу серверов) читают ответы из своих соединений и направляют их в канал ответов
func (nm *NetMutex) readResponses(s *server) {

	go nm.touch(s)

	for {
		// выходим из цикла, если клиент закончил свою работу
		select {
		case <-nm.done:
			s.conn.Close()
			return
		default:
		}

		// таймаут нужен для того, чтобы не залипнуть в чтении навечно, а можно было иногда от туда возвращаться,
		// например, чтобы корректно закончить работу клиента

		// Optimization: see https://github.com/golang/go/issues/15133 for details.
		currentTime := time.Now()
		if currentTime.Sub(s.lastReadDeadlineTime) > 59*time.Second {
			s.lastReadDeadlineTime = currentTime
			err := s.conn.SetReadDeadline(currentTime.Add(time.Minute))
			if err != nil {
				s.fail()
				continue
			}
		}

		resp, err := read(s)

		// если произошёл таймаут, выставленный строчкой выше, или ошибка временная
		if netErr, ok := err.(*net.OpError); ok {
			if netErr.Timeout() || netErr.Temporary() {
				continue
			}
		}

		if err != nil {
			// пример ошибки: read udp 127.0.0.1:19858->127.0.0.1:3002: read: connection refused
			s.fail()
			continue
		}

		// OPTIONS не привязана ни к какому запросу, поэтому обрабатывается отдельно
		if resp.code == code.OPTIONS {
			// переконфигурация: новый список сервероов, новый уникальный commandID
			// ToDo написать переконфигурацию
			putResponse(resp)
			continue
		}

		// находим запрос, соотвествующий ответу
		command, ok := nm.workingCommands.get(resp.id)
		// если команда не нашлась по ID, то ждём следующую
		if !ok {
			putResponse(resp)
			continue
		}
		command.processChan <- resp

	}
}

// пытается открыть соединение
func (nm *NetMutex) repairConn(server *server) {
	for {
		select {
		case <-nm.done:
			// выходим из цикла, если клиент закончил свою работу
			return

		default:
		}

		conn, err := openConn(server.addr)

		if err != nil {
			server.fail()
			time.Sleep(time.Minute)
			continue
		}
		server.conn = conn

		go nm.readResponses(server)
		return
	}
}

func (nm *NetMutex) connect(addr string, timeout time.Duration, isolationInfo string) (*response, error) {
	conn, err := openConn(addr)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	s := &server{
		id:   0,
		addr: addr,
		conn: conn,
	}

	req := &request{
		code:          code.CONNECT,
		isolationInfo: isolationInfo,
	}

	err = write(s, req)
	if err != nil {
		return nil, err
	}

	resp, err := readWithTimeout(s, timeout) // ToDo: вынести таймаут в Опции
	if err != nil {
		return nil, err
	}

	if resp.code != code.OPTIONS {
		return nil, errWrongResponse
	}

	return resp, nil
}

func (nm *NetMutex) ping(s *server, timeout time.Duration) error {
	req := &request{
		code: code.PING,
		id:   nm.commandID(),
	}

	err := write(s, req)
	if err != nil {
		return err
	}

	resp, err := readWithTimeout(s, timeout)
	if err != nil {
		return err
	}

	if resp.code != code.PONG || resp.id != req.id {
		return errWrongResponse
	}

	return nil
}

func (nm *NetMutex) commandID() commandID {
	return commandID{
		connectionID: nm.nextCommandID.connectionID,
		requestID:    atomic.AddUint64(&nm.nextCommandID.requestID, 1),
	}
}

// возвращает лучший из возможных серверов
func (nm *NetMutex) server() (*server, error) {
	nm.servers.Lock()
	defer nm.servers.Unlock()

	if nm.servers.current != nil {
		if atomic.LoadUint32(&nm.servers.current.fails) == 0 { // ToDo переписать. при множестве потерь пакетов это условие редко срабатывает и каждый раз делается перебор серверов
			return nm.servers.current, nil
		}
	}

	var bestFails uint32
	var bestServer *server
	for _, s := range nm.servers.m {
		// пропускаем несоединившиеся серверы
		if s.conn == nil {
			continue
		}

		// пропускаем текущий сервер
		if nm.servers.current == s {
			continue
		}

		if bestServer == nil {
			bestFails = atomic.LoadUint32(&s.fails)
			bestServer = s
		} else {
			curValue := atomic.LoadUint32(&s.fails)
			if bestFails > curValue {
				bestFails = curValue
				bestServer = s
			}
		}
	}

	if bestServer != nil {
		nm.servers.current = bestServer
		return nm.servers.current, nil
	}
	// если лучшего сервера не нашлось, а текущий имеется, то используем текущий сервер
	if nm.servers.current != nil {
		return nm.servers.current, nil
	}

	return nil, ErrNoServers
}

func (nm *NetMutex) serverByID(serverID uint64) *server {
	nm.servers.Lock()
	defer nm.servers.Unlock()

	if server, ok := nm.servers.m[serverID]; ok {
		return server
	}

	return nil
}
