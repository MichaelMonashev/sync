package netmutex

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/MichaelMonashev/sync/netmutex/checksum"
	"github.com/MichaelMonashev/sync/netmutex/code"
)

const (
	protocolHeaderSize = 32
	protocolTailSize   = checksum.Size
)

var zeroBuf = make([]byte, 24)
var zeroBuf400 = make([]byte, 400)

type command struct {
	id            commandID      // номер команды
	key           string         // ключ
	timeout       time.Duration  // за какое время надо попытаться выполнить команду
	ttl           time.Duration  // на сколько лочим ключ
	lockID        commandID      // номер команды блокировки при UNLOCK-е
	respChan      chan error     // канал, в который пишется ответ от сервера
	sendChan      chan *server   // канал, в который пишется сервер, на который надо послать запрос
	done          chan struct{}  // канал, закрытие которого извещает о завершении работы
	processChan   chan *response // канал, в который пишется ответ от сервера, который надо обработать
	netmutex      *NetMutex      // ссылка на клиента для чтения списка серверов и retries
	currentServer *server        // сервер, который обработывает текущий запрос
	timer         *time.Timer    // таймер текущего запроса
	retries       int            // количество запросов к серверам, прежде чем вернуть ошибку
	isolationInfo string         // информация об изоляции клиента
	code          byte           // код команды. Должно быть в хвосте структуры, ибо портит выравнивание всех остальных полей
	//t             time.Time// удалить. для дебага.
}

type commandID struct {
	serverID     uint64
	connectionID uint64
	requestID    uint64
}

func (command *command) marshalPacket(buf []byte, connID []byte, seqID uint64) (int, error) {

	n, err := command.marshalCommand(buf[protocolHeaderSize:])
	if err != nil {
		return 0, err
	}

	addHeaderAndTail(buf, n, connID, seqID)

	return protocolHeaderSize + n + protocolTailSize, nil
}

func addHeaderAndTail(buf []byte, n int, connID []byte, seqID uint64) {
	// записываем версию
	buf[0] = 1

	// записываем приоритет
	buf[1] = 128 // выше среднего

	// записываем код команды
	buf[2] = 0 // одиночный пакет

	//пока свободны и должны заполняться нулями
	buf[3] = 0
	buf[4] = 0
	buf[5] = 0
	buf[6] = 0
	buf[7] = 0

	// записываем длину
	binary.LittleEndian.PutUint64(buf[8:], uint64(protocolHeaderSize+n+protocolTailSize))

	// номер фрейма и номер пакета во фрейме
	//binary.LittleEndian.PutUint64(buf[16:], connID)
	copy(buf[16:], connID)                         // можно заменить нулями, ибо пока не используется никак
	binary.LittleEndian.PutUint64(buf[24:], seqID) // можно заменить нулями, ибо пока не используется никак

	// записываем контрольную сумму
	chsum := checksum.Checksum(buf[:protocolHeaderSize+n])
	copy(buf[protocolHeaderSize+n:], chsum[:])
}

func (req *command) marshalCommand(buf []byte) (int, error) {

	var bufSize int

	buf[0] = req.code

	switch req.code {
	case code.CONNECT:
		// size := расчитываем
		// code := CONNECT
		isolationInfoLen := len(req.isolationInfo)
		if isolationInfoLen > MaxIsolationInfo {
			return 0, ErrLongIsolationInfo
		}
		binary.LittleEndian.PutUint16(buf[1:], uint16(isolationInfoLen))

		copy(buf[3:3+isolationInfoLen], req.isolationInfo)

		copy(buf[3+isolationInfoLen:], zeroBuf) // зарезервированные на будущее 3 Uint64

		bufSize = isolationInfoLen + 3 + 24

	case code.PING:
		// size := 25+2+400
		// code := PING

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		//bufSize = 25

		binary.LittleEndian.PutUint16(buf[25:], 400) // ToDo точнее рассчитать от 508

		copy(buf[27:], zeroBuf400)

		bufSize = 27 + 400

	case code.TOUCH:
		// size := 25
		// code := TOUCH

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		bufSize = 25

	case code.DISCONNECT:
		// size := 25
		// code := DISCONNECT

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		bufSize = 25

	case code.LOCK:
		// size := расчитываем
		// code := LOCK

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		binary.LittleEndian.PutUint64(buf[25:], uint64(req.timeout))
		binary.LittleEndian.PutUint64(buf[33:], uint64(req.ttl))

		reqKey := req.key
		reqKeyLen := len(reqKey)
		if reqKeyLen > MaxKeySize {
			return 0, ErrLongKey
		}

		buf[41] = byte(reqKeyLen)

		copy(buf[42:42+reqKeyLen], reqKey)

		copy(buf[42+reqKeyLen:], zeroBuf) // зарезервированные на будущее 3 Uint64

		bufSize = reqKeyLen + 42 + 24

	case code.UPDATE:
		// size := расчитываем
		// code := UPDATE

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		binary.LittleEndian.PutUint64(buf[25:], uint64(req.timeout))

		binary.LittleEndian.PutUint64(buf[33:], req.lockID.serverID)
		binary.LittleEndian.PutUint64(buf[41:], req.lockID.connectionID)
		binary.LittleEndian.PutUint64(buf[49:], req.lockID.requestID)

		binary.LittleEndian.PutUint64(buf[57:], uint64(req.ttl))

		reqKey := req.key
		reqKeyLen := len(reqKey)
		if reqKeyLen > MaxKeySize {
			return 0, ErrLongKey
		}

		buf[65] = byte(reqKeyLen)

		copy(buf[66:66+reqKeyLen], reqKey)

		copy(buf[66+reqKeyLen:], zeroBuf) // зарезервированные на будущее 3 Uint64

		bufSize = reqKeyLen + 66 + 24

	case code.UNLOCK:
		// size := расчитываем
		// code := UNLOCK

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		binary.LittleEndian.PutUint64(buf[25:], uint64(req.timeout))

		binary.LittleEndian.PutUint64(buf[33:], req.lockID.serverID)
		binary.LittleEndian.PutUint64(buf[41:], req.lockID.connectionID)
		binary.LittleEndian.PutUint64(buf[49:], req.lockID.requestID)

		reqKey := req.key
		reqKeyLen := len(reqKey)
		if reqKeyLen > MaxKeySize {
			return 0, ErrLongKey
		}

		buf[57] = byte(reqKeyLen)

		copy(buf[58:58+reqKeyLen], reqKey)

		bufSize = reqKeyLen + 58

	case code.UNLOCKALL:
		// size := 25
		// code := UNLOCKALL

		binary.LittleEndian.PutUint64(buf[1:], req.id.serverID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[17:], req.id.requestID)

		bufSize = 25

	default:
		return 0, errorln("Wrong command code:", req.code, ".")
	}

	return bufSize, nil
}

func (command *command) isEnoughRetries() bool {
	command.retries--
	return command.retries <= 0
}

func (command *command) run() {
	command.send(nil)

	for {
		select {
		case <-command.netmutex.done:
			return

		case server := <-command.sendChan:
			command.send(server)

		case <-command.timer.C:
			if command.onTimeout() {
				return
			}

		case <-command.done:
			return

		case resp := <-command.processChan:
			if command.process(resp) {
				return
			}
		}
	}
}

func (command *command) send(server *server) {

	var err error

	for {
		if server == nil {
			server, err = command.netmutex.server()
			if err != nil { // некуда отправлять команду, поэтому сразу возвращаем ошибку
				command.done <- struct{}{}
				command.respChan <- err
				return
			}
		}

		command.currentServer = server

		command.timer.Reset(command.timeout)

		//err = writeBuffered(server, command)
		err = write(server, command)
		if err == nil { // если нет ошибки, то выходим из функции и ждём прихода ответа или срабатывания таймера
			return
		}

		command.timer.Stop()

		// если ошибка в том, что длина ключа слишком велика, то помечать сервер плохим не нужно
		if err != ErrLongKey {
			command.currentServer.fail()
		}

		if command.isEnoughRetries() {
			command.respChan <- ErrTooMuchRetries
			return
		}

		server = nil // чтобы выбрать другой сервер
	}
}

// функция, вызывается, когда истёт таймаут на приход ответа от сервера
func (command *command) onTimeout() bool {
	command.currentServer.fail()

	if command.isEnoughRetries() {
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
	case code.OK:
		command.currentServer.ok()
		command.respChan <- nil

	case code.DISCONNECTED:
		command.currentServer.ok()
		command.respChan <- ErrDisconnected

	case code.ISOLATED:
		command.currentServer.ok()
		command.respChan <- ErrIsolated

	case code.LOCKED:
		command.currentServer.ok()
		command.respChan <- ErrLocked

	case code.REDIRECT:
		command.currentServer.ok()
		if command.isEnoughRetries() {
			command.respChan <- ErrTooMuchRetries
		} else {
			command.sendChan <- command.netmutex.serverByID(resp.serverID)
			return false
		}

	case code.ERROR:
		command.currentServer.ok()
		command.respChan <- errors.New(resp.description)

		// Важно!!!
		// Большая нагрузка на сервер должна обрабатываться не здесь и не так.
		// BUSY - команда транспортного уровня
		// Она должна приводить к паузе, а не к смене сервера
		//	case BUSY:
		//		command.currentServer.fail()
		//
		//		if command.isEnoughRetries() {
		//			command.respChan <- ErrBusy
		//		} else { // повторяем запрос к загруженному серверу
		//			command.sendChan <- command.currentServer
		//			return false
		//		}

	default:
		command.respChan <- errWrongResponse
	}

	putResponse(resp)

	return true
}
