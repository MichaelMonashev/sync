package netmutex

import (
	"crypto/rand"
	"net"
	"sync/atomic"
	"time"
)

type server struct {
	id      uint64
	addr    string
	conn    *net.UDPConn // ToDo под виндой в Go нельзя делать одновременно несколько recv() с одним коннектом(дескриптором). Можно попробовать создать несколько коннектов, може тогда будет отправляться в сумме больше запросов.
	frameID []byte       // ID соединения (случайное 64-битное число, генерится клиентом)
	seqID   uint64       // последовательный номер пакета в соединении
	fails   uint32
	busy    int32
	lastReadDeadlineTime time.Time
}


func (server *server) fail() {
	atomic.AddUint32(&server.fails, 1)
}

func (server *server) ok() {
	atomic.StoreUint32(&server.fails, 0)
}

// возвращает следующий номер пакета в соединении
func (server *server) getSeqID() uint64 {
	return atomic.AddUint64(&server.seqID, 1)
}

// генерит случайное ID соединения
func genFrameID() ([]byte, error) {

	buf := make([]byte, 8)

	_, err := rand.Read(buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}
