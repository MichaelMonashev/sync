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

type request struct {
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
}

type commandID struct {
	serverID     uint64
	connectionID uint64
	requestID    uint64
}

func (req *request) marshalPacket(buf []byte, connID []byte, seqID uint64) (int, error) {

	n, err := req.marshalCommand(buf[protocolHeaderSize:])
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

func (req *request) marshalCommand(buf []byte) (int, error) {

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
		return 0, errorln("Wrong req code:", req.code, ".")
	}

	return bufSize, nil
}

func (req *request) isEnoughRetries() bool {
	req.retries--
	return req.retries <= 0
}

func (req *request) run() {
	req.send(nil)

	for {
		select {
		case <-req.netmutex.done:
			return

		case server := <-req.sendChan:
			req.send(server)

		case <-req.timer.C:
			if req.onTimeout() {
				return
			}

		case <-req.done:
			return

		case resp := <-req.processChan:
			if req.process(resp) {
				return
			}
		}
	}
}

func (req *request) send(server *server) {

	var err error

	for {
		if server == nil {
			server, err = req.netmutex.server()
			if err != nil { // некуда отправлять команду, поэтому сразу возвращаем ошибку
				req.done <- struct{}{}
				req.respChan <- err
				return
			}
		}

		req.currentServer = server

		req.timer.Reset(req.timeout)

		err = write(server, req)
		if err == nil { // если нет ошибки, то выходим из функции и ждём прихода ответа или срабатывания таймера
			return
		}

		req.timer.Stop()

		// если ошибка в том, что длина ключа слишком велика, то помечать сервер плохим не нужно
		if err != ErrLongKey {
			req.currentServer.fail()
		}

		if req.isEnoughRetries() {
			req.respChan <- ErrTooMuchRetries
			return
		}

		server = nil // чтобы выбрать другой сервер
	}
}

// функция, вызывается, когда истёт таймаут на приход ответа от сервера
func (req *request) onTimeout() bool {
	req.currentServer.fail()

	if req.isEnoughRetries() {
		req.respChan <- ErrTooMuchRetries
	} else {
		req.sendChan <- nil
		return false
	}
	return true
}

func (req *request) process(resp *response) bool {
	req.timer.Stop()

	switch resp.code {
	case code.OK:
		req.currentServer.ok()
		req.respChan <- nil

	case code.DISCONNECTED:
		req.currentServer.ok()
		req.respChan <- ErrDisconnected

	case code.ISOLATED:
		req.currentServer.ok()
		req.respChan <- ErrIsolated

	case code.LOCKED:
		req.currentServer.ok()
		req.respChan <- ErrLocked

	case code.REDIRECT:
		req.currentServer.ok()
		if req.isEnoughRetries() {
			req.respChan <- ErrTooMuchRetries
		} else {
			req.sendChan <- req.netmutex.serverByID(resp.serverID)
			return false
		}

	case code.ERROR:
		req.currentServer.ok()
		req.respChan <- errors.New(resp.description)

	default:
		req.respChan <- errWrongResponse
	}

	putResponse(resp)

	return true
}
