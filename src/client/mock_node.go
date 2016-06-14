package client

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"time"
)

func Mock_start_node(node_id uint64, mosk_nodes map[uint64]string) error {

	addr, err := net.ResolveUDPAddr("udp", mosk_nodes[node_id])
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	go mock_run(conn, node_id, mosk_nodes)

	return nil
}

func mock_run(conn *net.UDPConn, node_id uint64, mosk_nodes map[uint64]string) {
	for {
		buf := acquire_byte_buffer()

		_, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			warn(err)
			continue
		}

		switch buf[3] {
		case CONNECT:
			go mock_on_connect(conn, addr, buf, node_id, mosk_nodes)

		case LOCK, UNLOCK:
			go mock_on_lock_unlock(conn, addr, buf)

		case PING:
			go mock_on_ping(conn, addr, buf)

		default:
			warn("Wrong command", fmt.Sprint(buf[3]), "from", addr, conn.LocalAddr(), conn.RemoteAddr())
		}
	}
}

func mock_on_connect(conn *net.UDPConn, addr *net.UDPAddr, buf []byte, node_id uint64, mosk_nodes map[uint64]string) {
	defer release_byte_buffer(buf)
	buf[3] = OPTIONS

	binary.LittleEndian.PutUint64(buf[8:], node_id)
	binary.LittleEndian.PutUint64(buf[16:], 1)
	binary.LittleEndian.PutUint64(buf[24:], 0)

	nodes_pos := 32
	buf[nodes_pos] = byte(len(mosk_nodes))
	nodes_pos++
	for node_id, node_string := range mosk_nodes {
		// id ноды
		binary.LittleEndian.PutUint64(buf[nodes_pos:], node_id)
		nodes_pos += 8

		// длина адреса ноды
		buf[nodes_pos] = byte(len(node_string))
		nodes_pos++

		// адрес ноды
		copy(buf[nodes_pos:], node_string)
		nodes_pos += len(node_string)
	}

	_, err := conn.WriteToUDP(buf, addr)
	if err != nil {
		warn(err)
	}

}

func mock_on_lock_unlock(conn *net.UDPConn, addr *net.UDPAddr, buf []byte) {
	defer release_byte_buffer(buf)
	buf[3] = OK

	// выставляем длину пакета
	binary.LittleEndian.PutUint16(buf[1:], 32)

	// ждём некоторое время
	time.Sleep(time.Duration(rand.Intn(110)) * time.Millisecond)

	_, err := conn.WriteToUDP(buf[:32], addr)
	if err != nil {
		warn(err)
	}
}

func mock_on_ping(conn *net.UDPConn, addr *net.UDPAddr, buf []byte) {
	defer release_byte_buffer(buf)
	buf[3] = PONG
	_, err := conn.WriteToUDP(buf, addr)
	if err != nil {
		warn(err)
	}
}
