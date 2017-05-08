// Package netmutex implements low-level high performance client library for Taooka distributed lock manager (http://taooka.com/).
// It is very important to correctly handle errors that return functions!!!
package netmutex

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MichaelMonashev/sync/netmutex/code"
)

// Size limits.
const (
	// MaxKeySize - maximum key size.
	MaxKeySize = 255

	// MaxIsolationInfo - maximum length of information for client isolation. Taooka passes it to STDIN of "isolate" programm while client broken.
	MaxIsolationInfo = 400
)

// Returned errors.
var (
	// ErrDisconnected - the connection was closed.
	ErrDisconnected = errors.New("Client connection had closed.")

	// ErrIsolated - the client was isolated. You need to quit the program.
	ErrIsolated = errors.New("Client had isolated.")

	// ErrLocked - the key was locked by someone else.
	ErrLocked = errors.New("Key locked.")

	// ErrNoServers - could not connect to any server from the list or all of them became unavailable.
	ErrNoServers = errors.New("No working servers.")

	// ErrTooMuchRetries - the number of attempts to send a command to the server has been exceeded.
	ErrTooMuchRetries = errors.New("Too much retries.")

	// ErrLongKey - the key is longer than MaxKeySize bytes.
	ErrLongKey = errors.New("Key too long.")

	// ErrWrongTTL - TTL is less than zero.
	ErrWrongTTL = errors.New("Wrong TTL.")

	// ErrLongIsolationInfo - information for client isolation is longer than MaxIsolationInfo bytes.
	ErrLongIsolationInfo = errors.New("Client isolation information too long.")
)

var (
	errWrongResponse = errors.New("Wrong response.")
	errWrongLock     = errors.New("Use l := nm.NewLock() instead of l := &Lock{}.")
)

type servers struct {
	sync.Mutex
	m       map[uint64]*server
	current *server
}

// Lock — lock object.
type Lock struct {
	key       string
	commandID commandID
	timeout   time.Duration
	nm        *NetMutex
}

// NewLock return new lock object.
func (nm *NetMutex) NewLock() *Lock {
	return &Lock{
		nm: nm,
	}
}

// Options specifies additional connection parameters.
type Options struct {
	IsolationInfo string // Information about how the client will be isolated from the data it is changing in case of non-operation.
}

// NetMutex implements network locks.
type NetMutex struct {
	nextCommandID   commandID // должна быть первым полем в структуре, иначе может быть неверное выравнивание и atomic перестанет работать
	done            chan struct{}
	servers         *servers
	workingCommands *workingCommands
}

// Open tries during timeout to connect to any server from the list of addrs, get the current list of servers from it using options. If not, then repeat the bypass retries once. If so, then it tries to open a connection to each server from the list of servers received.
func Open(retries int, timeout time.Duration, addrs []string, options *Options) (*NetMutex, error) {

	nm := &NetMutex{
		done:            make(chan struct{}),
		workingCommands: newWorkingCommands(),
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

// Lock tries to lock the key, making no more retries of attempts, during each waiting for a response from the server during the timeout. If the lock succeeds, it is written to l.
func (l *Lock) Lock(retries int, timeout time.Duration, key string, ttl time.Duration) error {

	if l.nm == nil {
		return errWrongLock
	}

	if len(key) > MaxKeySize {
		return ErrLongKey
	}

	if ttl < 0 {
		return ErrWrongTTL
	}

	nm := l.nm

	id := nm.commandID()

	err := nm.runCommand(key, id, code.LOCK, timeout, ttl, commandID{}, retries)
	if err != nil {
		return err
	}

	l.key = key
	l.commandID = id
	l.timeout = timeout

	return nil

}

// Update tries to update the ttl of the l lock, making no more retries of attempts, during each waiting for a response from the server during the timeout. Allows you to extend the lifetime of the lock. Suitable for the implementation of heartbeat, which allows optimal control of the ttl key.
func (l *Lock) Update(retries int, timeout time.Duration, ttl time.Duration) error {
	if l.nm == nil {
		return errWrongLock
	}

	if len(l.key) > MaxKeySize {
		return ErrLongKey
	}

	if ttl < 0 {
		return ErrWrongTTL
	}

	nm := l.nm

	return nm.runCommand(l.key, nm.commandID(), code.UPDATE, timeout, ttl, l.commandID, retries)
}

//Unlock tries to unlock l, making no more retries of attempts, during each waiting for a response from the server during the timeout.
func (l *Lock) Unlock(retries int, timeout time.Duration) error {
	if l.nm == nil {
		return errWrongLock
	}

	if len(l.key) > MaxKeySize {
		return ErrLongKey
	}

	nm := l.nm

	return nm.runCommand(l.key, nm.commandID(), code.UNLOCK, l.timeout, 0, l.commandID, retries)
}

//UnlockAll tries to remove all locks by making no more retries of attempts, during each waiting for a response from the server during the timeout.
//Use with caution! Clients with existing locks will be isolated!
func (nm *NetMutex) UnlockAll(retries int, timeout time.Duration) error {
	return nm.runCommand("", nm.commandID(), code.UNLOCKALL, timeout, 0, commandID{}, retries)
}

// Close closes the connection to the lock server.
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
