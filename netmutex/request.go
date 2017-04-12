package netmutex

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/MichaelMonashev/sync/netmutex/checksum"
	"github.com/MichaelMonashev/sync/netmutex/code"
)

const (
	protocolHeaderSize = 4
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
	connectionID uint64
	requestID    uint64
}

func (req *request) marshalPacket(buf []byte) (int, error) {

	n, err := req.marshalCommand(buf[protocolHeaderSize:])
	if err != nil {
		return 0, err
	}

	addHeaderAndTail(buf, n)

	return protocolHeaderSize + n + protocolTailSize, nil
}

func addHeaderAndTail(buf []byte, n int) {
	// записываем версию
	buf[0] = 1

	// записываем флаги
	buf[1] = 0

	// записываем длину
	binary.LittleEndian.PutUint16(buf[2:], uint16(protocolHeaderSize+n+protocolTailSize))

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

		copy(buf[3:], req.isolationInfo)

		bufSize = isolationInfoLen + 3

	case code.PING:
		// size := 1+16+2+400
		// code := PING

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		//bufSize = 25

		binary.LittleEndian.PutUint16(buf[17:], 400) // ToDo точнее рассчитать от 508

		copy(buf[19:], zeroBuf400)

		bufSize = 19 + 400

	case code.TOUCH:
		// size := 1+16
		// code := TOUCH

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		bufSize = 17

	case code.DISCONNECT:
		// size := 1+16
		// code := DISCONNECT

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		bufSize = 17

	case code.LOCK:
		// size := расчитываем
		// code := LOCK

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		binary.LittleEndian.PutUint64(buf[17:], uint64(req.ttl))

		reqKey := req.key
		reqKeyLen := len(reqKey)
		if reqKeyLen > MaxKeySize {
			return 0, ErrLongKey
		}

		buf[25] = byte(reqKeyLen)

		copy(buf[26:26+reqKeyLen], reqKey)

		bufSize = 1 + 16 + 8 + 1 + reqKeyLen

	case code.UPDATE:
		// size := расчитываем
		// code := UPDATE

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		binary.LittleEndian.PutUint64(buf[17:], req.lockID.connectionID)
		binary.LittleEndian.PutUint64(buf[25:], req.lockID.requestID)

		binary.LittleEndian.PutUint64(buf[33:], uint64(req.ttl))

		reqKey := req.key
		reqKeyLen := len(reqKey)
		if reqKeyLen > MaxKeySize {
			return 0, ErrLongKey
		}

		buf[41] = byte(reqKeyLen)

		copy(buf[42:42+reqKeyLen], reqKey)

		bufSize = 1 + 16 + 16 + 8 + 1 + reqKeyLen

	case code.UNLOCK:
		// size := расчитываем
		// code := UNLOCK

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		binary.LittleEndian.PutUint64(buf[17:], req.lockID.connectionID)
		binary.LittleEndian.PutUint64(buf[25:], req.lockID.requestID)

		reqKey := req.key
		reqKeyLen := len(reqKey)
		if reqKeyLen > MaxKeySize {
			return 0, ErrLongKey
		}

		buf[33] = byte(reqKeyLen)

		copy(buf[34:34+reqKeyLen], reqKey)

		bufSize = 1 + 16 + 16 + 1 + reqKeyLen

	case code.UNLOCKALL:
		// size := 1+16
		// code := UNLOCKALL

		binary.LittleEndian.PutUint64(buf[1:], req.id.connectionID)
		binary.LittleEndian.PutUint64(buf[9:], req.id.requestID)

		bufSize = 1 + 16

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

		// если ошибка в том, что длина ключа слишком велика, то помечать сервер плохим не нужно.
		// На самом деле все эти ошибки отлавливаются ещё раньше и тут эти проверки не нужны.
		if err != ErrLongKey && err != ErrLongIsolationInfo {
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
