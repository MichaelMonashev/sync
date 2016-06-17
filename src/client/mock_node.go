package client

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
	//"math/rand"
	//"time"
)

func Mock_start_node(node_id uint64, mosk_nodes map[uint64]string) (chan bool, error) {

	addr, err := net.ResolveUDPAddr("udp", mosk_nodes[node_id])
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	done := make(chan bool)

	go mock_run(conn, node_id, mosk_nodes, done)

	return done, nil
}

func Mock_stop_node(done chan bool) {
	close(done)
	time.Sleep(200 * time.Millisecond) // ждём, пока закончится цикл в mock_run()
}

func mock_run(conn *net.UDPConn, node_id uint64, mosk_nodes map[uint64]string, done chan bool) {
	for {
		// выходим из цикла, если надо закончить свою работу
		select {
		case <-done:
			conn.Close()
			return
		default:
		}

		b := acquire_byte_buffer()

		// deadline нужен чтобы можно было завершить выйти из цикла и завершить работу
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
			go mock_on_connect(conn, addr, b, node_id, mosk_nodes)

		case LOCK, UNLOCK:
			go mock_on_lock_unlock(conn, addr, b)

		case PING:
			go mock_on_ping(conn, addr, b)

		default:
			warn("Wrong command", fmt.Sprint(b.buf[3]), "from", addr, conn.LocalAddr(), conn.RemoteAddr())
		}
	}
}

func mock_on_connect(conn *net.UDPConn, addr *net.UDPAddr, b *byte_buffer, node_id uint64, mosk_nodes map[uint64]string) {
	defer release_byte_buffer(b)
	b.buf[3] = OPTIONS

	binary.LittleEndian.PutUint64(b.buf[8:], node_id)
	binary.LittleEndian.PutUint64(b.buf[16:], 1)
	binary.LittleEndian.PutUint64(b.buf[24:], 0)

	nodes_pos := 32
	b.buf[nodes_pos] = byte(len(mosk_nodes))
	nodes_pos++
	for node_id, node_string := range mosk_nodes {
		// id ноды
		binary.LittleEndian.PutUint64(b.buf[nodes_pos:], node_id)
		nodes_pos += 8

		// длина адреса ноды
		b.buf[nodes_pos] = byte(len(node_string))
		nodes_pos++

		// адрес ноды
		copy(b.buf[nodes_pos:], node_string)
		nodes_pos += len(node_string)
	}

	_, err := conn.WriteToUDP(b.buf, addr)
	if err != nil {
		warn(err)
	}

}

func mock_on_lock_unlock(conn *net.UDPConn, addr *net.UDPAddr, b *byte_buffer) {
	defer release_byte_buffer(b)
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

func mock_on_ping(conn *net.UDPConn, addr *net.UDPAddr, b *byte_buffer) {
	defer release_byte_buffer(b)
	b.buf[3] = PONG
	_, err := conn.WriteToUDP(b.buf, addr)
	if err != nil {
		warn(err)
	}
}
