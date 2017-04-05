package netmutex

import (
	"net"
	"sync/atomic"
	"time"
)

type server struct {
	id      uint64
	addr    string
	conn    *net.UDPConn // ToDo под виндой в Go нельзя делать одновременно несколько recv() с одним коннектом(дескриптором). Можно попробовать создать несколько коннектов, може тогда будет отправляться в сумме больше запросов.
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
