package netmutex

import (
	"net"
	"sync/atomic"
	"time"
)

const (
	maxBusy  = 10000 // соотвествует 1/10 секунды
	busyStep = 10 * time.Microsecond
)

func openConn(addr string) (*net.UDPConn, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}

	return net.DialUDP("udp", nil, serverAddr)
}

func write(s *server, req *request) error {
	// если сервер перегружен, то перед отправкой запроса сделаем небольшую паузу пропорционально его нагруженности
	serverBusy := atomic.LoadInt32(&s.busy)
	if serverBusy > 0 {
		time.Sleep(time.Duration(serverBusy) * busyStep)
	}

	b := getByteBuffer()
	defer putByteBuffer(b)

	n, err := req.marshalPacket(b.buf, s.frameID, s.getSeqID())
	if err != nil {
		return err
	}

	_, err = s.conn.Write(b.buf[0:n])
	return err
}

func readWithTimeout(s *server, timeout time.Duration) (*response, error) {
	err := s.conn.SetReadDeadline(time.Now().Add(timeout))
	if err != nil {
		return nil, err
	}
	defer s.conn.SetReadDeadline(time.Time{}) // убираем таймаут для будущих операций

	return read(s)
}

func read(s *server) (*response, error) {

	b := getByteBuffer()
	defer putByteBuffer(b)

	n, err := s.conn.Read(b.buf)
	if err != nil {
		return nil, err
	}

	response := getResponse()
	busy, err := response.unmarshalPacket(b.buf[:n])
	if busy {
		if atomic.LoadInt32(&s.busy) < maxBusy { // s.busy может стать немножко больше maxBusy, но не кардинально, так что это не сташно
			atomic.AddInt32(&s.busy, 1) // ToDo: в зависимости от s.busy прибавлять разное число. 1 соотвествует 10 микросекундам паузы перед обращением к серверу.
		}
	} else if atomic.LoadInt32(&s.busy) > 0 { // s.busy может немножко уйти в минус, но при задании паузы мы это учтём
		atomic.AddInt32(&s.busy, -1)
	}

	if err != nil {
		putResponse(response)
		return nil, err
	}

	return response, nil
}
