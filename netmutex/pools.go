package netmutex

import (
	"sync"
	"time"
)

//var lockPool sync.Pool
//
//func getLock() *Lock {
//	l := lockPool.Get()
//	if l == nil {
//		return &Lock{}
//	}
//	return l.(*Lock)
//}
//
//func putLock(l *Lock) {
//	nilCheck(l)
//
//	lockPool.Put(l)
//}

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
			//retries:     0,
		}
	}
	return req.(*request)
}

func putRequest(req *request) {
	nilCheck(req)

	commandPool.Put(req)
}

//----------------------------------------------
var responsePool sync.Pool

func getResponse() *response {
	r := responsePool.Get()
	if r == nil {
		return &response{
			servers: make(map[uint64]string),
		}
	}
	return r.(*response)
}

func putResponse(r *response) {
	nilCheck(r)

	for s := range r.servers {
		delete(r.servers, s)
	}

	responsePool.Put(r)
}

//----------------------------------------------
var accPool sync.Pool

type acc struct {
	buf []byte
	err chan error
}

func getAcc() *acc {
	b := accPool.Get()
	if b == nil {
		return &acc{
			buf: make([]byte, defaultByteBufferSize),
			err: make(chan error, 1),
		}
	}
	return b.(*acc)
}

func putAcc(b *acc) {
	nilCheck(b)

	b.buf = b.buf[0:defaultByteBufferSize]
	accPool.Put(b)
}
