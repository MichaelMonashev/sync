package netmutex

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

type callback func(*net.UDPConn, *net.UDPAddr, *byteBuffer)

var addresses []string = []string{
	"127.0.0.1:3001",
	"127.0.0.1:3002",
	"127.0.0.1:3003"}

func TestMain(m *testing.M) {
	flag.Parse()

	// запускаем правильно функционирующие ноды
	for i := 1; i <= 3; i++ {
		mockNode, err := mockStartNode(uint64(i), map[uint64]string{
			1: "127.0.0.1:3001",
			2: "127.0.0.1:3002",
			3: "127.0.0.1:3003",
		}, mockOnPing)

		if err != nil {
			log.Fatal(err)
		}
		defer mockStopNode(mockNode)

		log.Println("node", i, "successful started")
	}

	// запускаем кривую ноду, которая неправильно понгает
	mockNode1, err := mockStartNode(uint64(4), map[uint64]string{
		4: "127.0.0.1:3005",
	}, mockOnPingBad)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("node", 4, "successful started")
	defer mockStopNode(mockNode1)

	os.Exit(m.Run())
}

// штатная попытка соединения
func TestOpen1(t *testing.T) {
	nm, err := Open(addresses,
		nil)

	if err != nil {
		t.Fail()
	} else {
		nm.Close()
	}
}

// попытаемся соединиться с несуществующей нодой
// должна произойти ошибка
func TestOpen2(t *testing.T) {
	nm, err := Open([]string{
		"127.0.0.1:3004"},
		nil)

	if err == nil {
		t.Fail()
		nm.Close()
	}
}

// попытаемся соединиться с нодой, которая неправильно понгает
// у ноды fails увеличивается
func TestOpen3(t *testing.T) {
	nm, err := Open([]string{
		"127.0.0.1:3005"},
		nil)

	if err != nil {
		t.Fail()
	}
	defer nm.Close()

	if len(nm.nodes.m) != 1 {
		t.FailNow()
	}

	for _, v := range nm.nodes.m {
		if v.fails != 1 {
			t.Fail()
		}
	}
}

func TestCommandId(t *testing.T) {
	nm, err := Open(addresses, nil)

	if err != nil {
		t.Fail()
	}
	defer nm.Close()

	nodeID := nm.nextCommandID.nodeID
	connectionID := nm.nextCommandID.connectionID
	requestID := nm.nextCommandID.requestID

	nextCommandID := nm.commandID()

	if !(nextCommandID.nodeID == nodeID && nextCommandID.connectionID == connectionID && nextCommandID.requestID == requestID+1) {
		t.Fail()
	}
}

// normal operation
func TestConnectOptions1(t *testing.T) {
	netmutex := &NetMutex{
		ttl:             DefaultTTL,
		timeout:         DefaultTimeout,
		retries:         DefaultRetries,
		readBufferSize:  DefaultReadBufferSize,
		writeBufferSize: DefaultWriteBufferSize,
		done:            make(chan struct{}),
		responses:       make(chan *response),
		workingCommands: &workingCommands{
			m: make(map[commandID]*command),
		},
	}

	options, err := netmutex.connectOptions("127.0.0.1:3001")

	if err != nil {
		t.FailNow()
	}

	if options.code != OPTIONS {
		t.Error("bad code received")
	}
	if len(options.nodes) != 3 {
		t.Error("wrong number of nodes: want 3 got ", len(options.nodes))
	}
}

// соединиться с несуществующей нодой
func TestConnectOptions2(t *testing.T) {
	netmutex := &NetMutex{
		ttl:             DefaultTTL,
		timeout:         DefaultTimeout,
		retries:         DefaultRetries,
		readBufferSize:  DefaultReadBufferSize,
		writeBufferSize: DefaultWriteBufferSize,
		done:            make(chan struct{}),
		responses:       make(chan *response),
		workingCommands: &workingCommands{
			m: make(map[commandID]*command),
		},
	}

	_, err := netmutex.connectOptions("127.0.0.1:3004")

	fmt.Println(err)
	if err == nil {
		t.Error("must've been an error")
	}
}

// Lock/Unlock
func TestLock1(t *testing.T) {
	nm, err := Open(addresses, nil)

	if err != nil {
		t.Fail()
	}
	defer nm.Close()

	lock, err := nm.Lock("test")

	if err != nil {
		t.Fatal("can't lock", err)
	}

	err = nm.Unlock(lock)

	if err != nil {
		t.Fatal("can't unlock", err)
	}
}

// long key
func TestLock2(t *testing.T) {
	nm, err := Open(addresses, nil)

	if err != nil {
		t.Fail()
	}
	defer nm.Close()

	badKey := strings.Repeat("a", 300)

	_, err = nm.Lock(badKey)

	if err == nil {
		t.Fatal("must be error")
	}
	if err != ErrLongKey {
		t.Fatal("expecting ErrLongKey, got", err)
	}
}

func BenchmarkLock(b *testing.B) {

	locker, err := Open(addresses, nil)

	if err != nil {
		log.Fatal(err)
	}

	defer locker.Close()

	b.ResetTimer()

	key := "a"
	for n := 0; n < b.N; n++ {
		locker.Lock(key)
	}
}
func BenchmarkLockUnlock(b *testing.B) {

	locker, err := Open(addresses, nil)

	if err != nil {
		log.Fatal(err)
	}
	defer locker.Close()

	b.ResetTimer()

	key := "a"
	for n := 0; n < b.N; n++ {
		lock, _ := locker.Lock(key)
		if lock != nil {
			locker.Unlock(lock)
		}
	}
}

//
// mock nodes code
//
func mockStartNode(nodeID uint64, moskNodes map[uint64]string, pongFunc callback) (chan bool, error) {

	addr, err := net.ResolveUDPAddr("udp", moskNodes[nodeID])
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	done := make(chan bool)

	go mockRun(conn, nodeID, moskNodes, done, pongFunc)

	return done, nil
}

func mockStopNode(done chan bool) {
	close(done)
	time.Sleep(200 * time.Millisecond) // ждём, пока закончится цикл в mock_run()
}

func mockRun(conn *net.UDPConn, nodeID uint64, moskNodes map[uint64]string, done chan bool, pongFunc callback) {
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
			go pongFunc(conn, addr, b)

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

	// Код ответа
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

// ошибочный пинг-понг - не понгает в ответ на пинг
// нужно для тестирования правильности отработки коннекта
func mockOnPingBad(conn *net.UDPConn, addr *net.UDPAddr, b *byteBuffer) {
	defer releaseByteBuffer(b)
	b.buf[3] = TIMEOUT
	_, err := conn.WriteToUDP(b.buf, addr)
	if err != nil {
		warn(err)
	}
}

func warn(a ...interface{}) {
	fmt.Fprintln(os.Stderr, a)
}

// Keep this lines at the end of file

// go test -memprofile mem.out -memprofilerate=1 -benchmem -benchtime="10s" -bench="." netmutex -x
// go tool pprof --alloc_objects netmutex.test.exe mem.out

// go test -cpuprofile cpu.out -benchmem -benchtime="10s" -bench="." netmutex -x
// go tool pprof netmutex.test.exe cpu.out

// go test -cover -covermode=count -coverprofile=count.out
// go tool cover -html=count.out
