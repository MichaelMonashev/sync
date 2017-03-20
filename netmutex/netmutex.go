// Package netmutex implements low-level client library for distributed lock server.
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

const (
	// MaxKeySize - максимальная допустимая длина ключа.
	MaxKeySize = 255
	// MaxIsolationInfo - максимальная допустимая длина информации для изоляции клиента в случае его неработоспособности.
	MaxIsolationInfo = 400
)

// Коды возвращаемых ошибок.
var (
	// Соединение было закрыто ранее.
	ErrDisconnected = errors.New("Client connection had closed.")
	// Ключ заблокирован кем-то другим.
	ErrIsolated = errors.New("Client had isolated.")
	// Ключ заблокирован кем-то другим.
	ErrLocked = errors.New("Key locked.")
	// Не удалось подключиться ни к одному серверу из списка или все они стали недоступны.
	ErrNoServers = errors.New("No working servers.")
	// Превышено количество попыток отправить команду на сервер.
	ErrTooMuchRetries = errors.New("Too much retries.")
	// Ключ длиннее MaxKeySize байт.
	ErrLongKey = errors.New("Key too long.")
	// TTL меньше нуля.
	ErrWrongTTL = errors.New("Wrong TTL.")
	// Информация для изоляции клиента длиннее MaxIsolationInfo байт.
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

// Lock - блокировка.
type Lock struct {
	key       string
	commandID commandID
	timeout   time.Duration
}

// String возвращает текстовое представление блокировки.
func (l *Lock) String() string {
	return l.key
}

// Options задаёт дополнительные параметры соединения.
type Options struct {
	IsolationInfo string // Информация о том, как в случае неработоспособности будет изолироваться клиент.
	//	TTL             time.Duration // Time to live - значение по-умолчанию времени жизни блокировки.
	//	Timeout         time.Duration // Значение по-умолчанию для времени, за которое кластер распределённых блокировок будет пытаться установить или снять блокировку.
	//	Retries         int         // Количество попыток отправить запрос в кластер.

	//	ReadBufferSize  int // Sets the size of the operating system's receive buffer associated with connections.
	//	WriteBufferSize int // Sets the size of the operating system's transmit buffer associated with connections.
}

// NetMutex реализует блокировки по сети
type NetMutex struct {
	nextCommandID commandID // должна быть первым полем в структуре, иначе может быть неверное выравнивание и atomic перестанет работать
	//	ttl             time.Duration // значение по умолчанию для Lock(), Unlock()
	//	timeout         time.Duration // значение по умолчанию для Timeout
	//	retries         int
	//	readBufferSize  int
	//	writeBufferSize int
	done chan struct{}
	//	responses       chan *response
	servers         *servers
	workingCommands *workingCommands
}

// Open пытается за время timeout соединиться с любым сервером из списка addrs,
// получить с него актуальный список серверов, используя параметры options.
// Если не получается, то повторяет обход retries раз.
// Если получается, то пытается открыть соединение с каждым сервером из полученного списка серверов.
func Open(retries int, timeout time.Duration, addrs []string, options *Options) (*NetMutex, error) {

	nm := &NetMutex{
		done: make(chan struct{}),
		workingCommands: &workingCommands{
			m: make(map[commandID]*request),
		},
	}

	isolationInfo := ""
	if options != nil {
		isolationInfo = options.IsolationInfo
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

				frameID, err := genFrameID()
				if err != nil { // ошибка значит невозможность сгенерить номер соединения. скорее всего проблема не пропадёт на следующем сервере, так что лучше сразу вернуть ошибку
					return nil, err
				}

				s := &server{
					id:      serverID,
					addr:    serverAddr,
					frameID: frameID,
					// bufCh:  make(chan *acc, 500),
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

			//go nm.run()

			return nm, nil
		}
	}

	return nil, ErrNoServers
}

// заблокировать, если не было заблокировано ранее
//func (nm *NetMutex) LockIfUnlocked(key string) (*Lock, error) {
//	return nil, errors.New("LockIfUnlocked() has not yet implemented.")
//}

// заблокировать все или ниодного.
//func (nm *NetMutex) LockEach(keys []string) ([]*Lock, error) {
//	return nil, errors.New("LockEach() has not yet implemented.")
//}

// заблокировать всё, что получится.
//func (nm *NetMutex) LockAny(keys []string) ([]*Lock, error) {
//	return nil, errors.New("LockAny() has not yet implemented.")
//}

// заблокировать на чтение. Возможно два варианта реализации этой блокировки на сервере:
// 1) блокировка на запись устанавливается только если нет ниодной блокировки на чтение, т.е. при большом количестве блокировок на чтение блокировка на запись вообще никогда может не установиться.
// 2) новые блокировки на чтение не устанавливаются, если пытается установиться блокировка на запись
// 3) смешанный вариант: блокировка на запись несколько раз пытается установиться, если у неё это не выходит, то новые блокировки на чтение перестают устанавливаться на какое-то время, за которое блокировка на запись должна успеть установиться.
//func (nm *NetMutex) RLock(key string) (*Lock, error) {
//	return nm.Lock(key) // ToDo переделать
//}

// Получить по ключу существующую блокировку, чтобы можно было убрать чужие блокировки ранешь их истечения их TTL
//func (nm *NetMutex) GetLockID(key string) (*Lock, error) {
//	return nm.Lock(key) // ToDo переделать
//}

// Lock пытается заблокировать ключ key, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout.
func (nm *NetMutex) Lock(retries int, timeout time.Duration, key string, ttl time.Duration) (*Lock, error) {

	if len(key) > MaxKeySize {
		return nil, ErrLongKey
	}

	if ttl < 0 {
		return nil, ErrWrongTTL
	}

	id := nm.commandID()

	err := nm.runCommand(key, id, code.LOCK, timeout, ttl, commandID{}, retries)
	if err != nil {
		return nil, err
	}

	lock := getLock()

	lock.key = key
	lock.commandID = id
	lock.timeout = timeout

	return lock, nil

}

// Возвращает ключ, по которому произошла блокировка. Удобен в LockEach() и LockAny()
//func (lock *Lock) Key() string {
//	return lock.key
//}

// Update пытается у ключа key обновить ttl, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout.
// Позволяет продлять время жизни блокировки.
// Подходит для реализации heartbeat-а, позволяющего оптимальнее управлять ttl ключа.
func (nm *NetMutex) Update(retries int, timeout time.Duration, lock *Lock, ttl time.Duration) error {
	if lock == nil {
		return errLockIsNil
	}

	if ttl < 0 {
		return ErrWrongTTL
	}

	return nm.runCommand(lock.key, nm.commandID(), code.UPDATE, timeout, ttl, lock.commandID, retries)
}

// Unlock пытается снять блокировку у ключа key, сделав не более retries попыток, в течении каждой ожидая ответа от сервера в течении timeout.
func (nm *NetMutex) Unlock(retries int, timeout time.Duration, lock *Lock) error {
	if lock == nil {
		return errLockIsNil
	}

	defer putLock(lock)

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

	command.id = nm.commandID()
	command.code = code.TOUCH

	for {
		// выходим из цикла, если клиент закончил свою работу
		select {
		case <-nm.done:
			return
		default:
		}

		command.id = nm.commandID()

		write(s, command) // ответ именно для этого command.id нам не важен, так что не запускаем горутину, ждущую именно этот ответ.

		time.Sleep(10 * time.Minute) // ToDo Вынести в константы
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
				//warn(err)
				s.fail()
				continue
			}
		}

		resp, err := read(s)

		// таймаут нужен для того, чтобы не залипнуть в чтении навечно, а можно было иногда от туда возвращаться,
		// например, чтобы корректно закончить работу клиента
		//resp, err := readWithTimeout(s, time.Second)

		// если произошёл таймаут, выставленный строчкой выше, или ошибка временная
		if netErr, ok := err.(*net.OpError); ok {
			if netErr.Timeout() || netErr.Temporary() {
				continue
			}
		}

		if err != nil {
			// пример ошибки: read udp 127.0.0.1:19858->127.0.0.1:3002: read: connection refused
			//warn(err)
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

	frameID, err := genFrameID()
	if err != nil {
		return nil, err
	}

	s := &server{
		id:      0,
		addr:    addr,
		conn:    conn,
		frameID: frameID,
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
		serverID:     nm.nextCommandID.serverID,
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
	//bestServer := nm.servers.current
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
