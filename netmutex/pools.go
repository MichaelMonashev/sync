package netmutex

import (
	"sync"
	"time"
)

//----------------------------------------------
var byteBufferPool sync.Pool

const defaultByteBufferSize = 65536 // максимальный размер UDP-пакета с заголовками

type byteBuffer struct {
	buf []byte
}

func getByteBuffer() *byteBuffer {
	v := byteBufferPool.Get()
	if v == nil {
		return &byteBuffer{
			buf: make([]byte, defaultByteBufferSize),
		}
	}
	return v.(*byteBuffer)
}

func putByteBuffer(b *byteBuffer) {
	nilCheck(b)

	b.buf = b.buf[0:defaultByteBufferSize]
	byteBufferPool.Put(b)
}

//----------------------------------------------
var commandPool sync.Pool

func getRequest() *request {
	req := commandPool.Get()
	if req == nil {
		timer := time.NewTimer(time.Hour) // ??? как иначе создать таймер с каналом C != nil?
		timer.Stop()
		return &request{
			respChan:    make(chan error),
			sendChan:    make(chan *server, 2),
			processChan: make(chan *response),
			done:        make(chan struct{}),
			timer:       timer,
		}
	}
	return req.(*request)
}

func putRequest(req *request) {
	nilCheck(req)

	req.fenceID = 0

	commandPool.Put(req)
}

//----------------------------------------------
var responsePool sync.Pool

func getResponse() *response {
	resp := responsePool.Get()
	if resp == nil {
		return &response{
			servers: make(map[uint64]string),
		}
	}
	return resp.(*response)
}

func putResponse(resp *response) {
	nilCheck(resp)

	for s := range resp.servers {
		delete(resp.servers, s)
	}
	resp.fenceID = 0

	responsePool.Put(resp)
}
