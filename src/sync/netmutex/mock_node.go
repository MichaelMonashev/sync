package netmutex

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

func MockStartNode(nodeID uint64, moskNodes map[uint64]string) (chan bool, error) {

	addr, err := net.ResolveUDPAddr("udp", moskNodes[nodeID])
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	done := make(chan bool)

	go mockRun(conn, nodeID, moskNodes, done)

	return done, nil
}

func MockStopNode(done chan bool) {
	close(done)
	time.Sleep(200 * time.Millisecond) // ждём, пока закончится цикл в mock_run()
}

func mockRun(conn *net.UDPConn, nodeID uint64, moskNodes map[uint64]string, done chan bool) {
	for {
		// выходим из цикла, если надо закончить свою работу
		select {
		case <-done:
			conn.Close()
			return
		default:
		}

		b := acquireByteBuffer()

		// deadline нужен чтобы можно было выйти из цикла и завершить работу
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		_, addr, err := conn.ReadFromUDP(b.buf)

		if err != nil {
			// если произошёл не таймаут, а что-то другое
			if neterr, ok := err.(*net.OpError); !ok || !neterr.Timeout() {
				warn(err)
			}
			continue
		}

		switch b.buf[3] {
		case CONNECT:
			go mockOnConnect(conn, addr, b, nodeID, moskNodes)

		case LOCK, UNLOCK:
			go mockOnLockUnlock(conn, addr, b)

		case PING:
			go mockOnPing(conn, addr, b)

		default:
			warn("Wrong command", fmt.Sprint(b.buf[3]), "from", addr, conn.LocalAddr(), conn.RemoteAddr())
		}
	}
}

func mockOnConnect(conn *net.UDPConn, addr *net.UDPAddr, b *byteBuffer, nodeID uint64, moskNodes map[uint64]string) {
	defer releaseByteBuffer(b)
	b.buf[3] = OPTIONS

	binary.LittleEndian.PutUint64(b.buf[8:], nodeID)
	binary.LittleEndian.PutUint64(b.buf[16:], 1)
	binary.LittleEndian.PutUint64(b.buf[24:], 0)

	nodesPos := 32
	b.buf[nodesPos] = byte(len(moskNodes))
	nodesPos++
	for nodeID, nodeString := range moskNodes {
		// id ноды
		binary.LittleEndian.PutUint64(b.buf[nodesPos:], nodeID)
		nodesPos += 8

		// длина адреса ноды
		b.buf[nodesPos] = byte(len(nodeString))
		nodesPos++

		// адрес ноды
		copy(b.buf[nodesPos:], nodeString)
		nodesPos += len(nodeString)
	}

	_, err := conn.WriteToUDP(b.buf, addr)
	if err != nil {
		warn(err)
	}

}

func mockOnLockUnlock(conn *net.UDPConn, addr *net.UDPAddr, b *byteBuffer) {
	defer releaseByteBuffer(b)
	b.buf[3] = OK

	// выставляем длину пакета
	binary.LittleEndian.PutUint16(b.buf[1:], 32)

	// ждём некоторое время
	//time.Sleep(time.Duration(rand.Intn(110)) * time.Millisecond)

	_, err := conn.WriteToUDP(b.buf[:32], addr)
	if err != nil {
		warn(err)
	}
}

func mockOnPing(conn *net.UDPConn, addr *net.UDPAddr, b *byteBuffer) {
	defer releaseByteBuffer(b)
	b.buf[3] = PONG
	_, err := conn.WriteToUDP(b.buf, addr)
	if err != nil {
		warn(err)
	}
}
