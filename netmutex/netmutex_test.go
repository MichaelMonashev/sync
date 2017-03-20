package netmutex

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MichaelMonashev/sync/netmutex/checksum"
	"github.com/MichaelMonashev/sync/netmutex/code"
)

type callback func(*net.UDPConn, *net.UDPAddr, []byte)

var addresses []string = []string{
	"127.0.0.1:3001",
	"127.0.0.1:3002",
	"127.0.0.1:3003"}

func TestMain(m *testing.M) {
	// запускаем правильно функционирующие сервера
	for i := 1; i <= 3; i++ {
		mockServer, err := mockStartServer(uint64(i), map[uint64]string{
			1: "127.0.0.1:3001",
			2: "127.0.0.1:3002",
			3: "127.0.0.1:3003",
		}, mockOk)

		if err != nil {
			log.Fatal(err)
		}
		defer mockStopServer(mockServer)

		fmt.Println("mock server", i, "successful started")
	}

	// запускаем кривой сервер, который неправильно понгает
	mockServer1, err := mockStartServer(uint64(4), map[uint64]string{
		4: "127.0.0.1:3005",
	}, mockLocked)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("mock server", 4, "successful started")
	defer mockStopServer(mockServer1)

	os.Exit(m.Run())
}

// штатная попытка соединения
func TestOpen1(t *testing.T) {
	nm, err := Open(10, time.Minute, addresses,
		nil)

	if err != nil {
		t.Fatal(err)
	}

	nm.Close(1, time.Minute)
}

// попытаемся соединиться с несуществующим сервером
// должна произойти ошибка
func TestOpen2(t *testing.T) {
	nm, err := Open(10, time.Second, []string{
		"127.0.0.1:3004"},
		nil)

	if err == nil {
		nm.Close(1, time.Second)
		t.Fatal()
	}
}

// попытаемся соединиться с сервером, который неправильно понгает
// у сервера fails должен увеличиться
func TestOpen3(t *testing.T) {
	nm, err := Open(10, time.Minute, []string{
		"127.0.0.1:3005"},
		nil)

	if err != nil {
		t.Fatal(err)
	}
	defer nm.Close(1, time.Minute)

	if len(nm.servers.m) != 1 {
		t.FailNow()
	}

	for _, v := range nm.servers.m {
		if v.fails != 1 {
			t.Fatal()
		}
	}
}

func TestCommandId(t *testing.T) {
	nm, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer nm.Close(1, time.Minute)

	serverID := nm.nextCommandID.serverID
	connectionID := nm.nextCommandID.connectionID
	requestID := atomic.LoadUint64(&nm.nextCommandID.requestID)

	nextCommandId := nm.commandID()

	if !(nextCommandId.serverID == serverID && nextCommandId.connectionID == connectionID && nextCommandId.requestID == requestID+1) {
		t.Fatal()
	}
}

// normal operation
func TestConnect1(t *testing.T) {
	netmutex := &NetMutex{
		//		ttl:             DefaultTTL,
		//		timeout:         DefaultTimeout,
		//		retries:         DefaultRetries,
		//		readBufferSize:  DefaultReadBufferSize,
		//		writeBufferSize: DefaultWriteBufferSize,
		done: make(chan struct{}),
		workingCommands: &workingCommands{
			m: make(map[commandID]*request),
		},
	}

	options, err := netmutex.connect("127.0.0.1:3001", time.Minute, "")

	if err != nil {
		t.Fatal(err)
	}

	if options.code != code.OPTIONS {
		t.Fatal("bad code received")
	}
	if len(options.servers) != 3 {
		t.Fatal("wrong number of servers: want 3 got ", len(options.servers), options.servers)
	}
}

// соединиться с несуществующим сервером
func TestConnect2(t *testing.T) {
	netmutex := &NetMutex{
		//		ttl:             DefaultTTL,
		//		timeout:         DefaultTimeout,
		//		retries:         DefaultRetries,
		//		readBufferSize:  DefaultReadBufferSize,
		//		writeBufferSize: DefaultWriteBufferSize,
		done: make(chan struct{}),
		workingCommands: &workingCommands{
			m: make(map[commandID]*request),
		},
	}

	_, err := netmutex.connect("127.0.0.1:3004", time.Second, "")

	fmt.Println(err)
	if err == nil {
		t.Fatal("must've been an error")
	}
}

// Lock/Unlock
func TestLock1(t *testing.T) {
	nm, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer nm.Close(1, time.Minute)

	retries := 10
	timeout := time.Minute
	ttl := time.Second

	key := "test"

	for i := 1000; i > 0; i-- {
		lock, err := nm.Lock(retries, timeout, key, ttl)

		if err != nil {
			t.Fatal("can't lock", err)
		}

		err = nm.Unlock(retries, timeout, lock)

		if err != nil {
			t.Fatal("can't unlock", err)
		}
	}
}

// long key
func TestLock2(t *testing.T) {
	nm, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer nm.Close(1, time.Minute)

	retries := 10
	timeout := time.Minute
	ttl := time.Second

	bad_key := strings.Repeat("a", 300)

	_, err = nm.Lock(retries, timeout, bad_key, ttl)

	if err == nil {
		t.Fatal("must be error")
	}
	if err != ErrLongKey {
		t.Fatal("expecting ErrLongKey, got", err)
	}
}

// делает кучу аллокаций
//func BenchmarkLock(b *testing.B) {
//
//	locker, err := Open(10,time.Second,addresses, nil)
//	if err != nil {
//		b.Fatal(err)
//	}
//
//	defer locker.Close()
//
//	b.ResetTimer()
//
//	retries := 10
//	timeout := time.Minute
//	ttl := time.Second
//
//	key := "a"
//	for n := 0; n < b.N; n++ {
//		locker.Lock(retries, timeout, key, ttl)
//	}
//}

// On FreeBSD 11.0:
// BenchmarkUDPWrite-4  3000000  4158 ns/op  33.66 MB/s  0 B/op  0 allocs/op
//
// On Ubuntu 16.10:
// BenchmarkUDPWrite-4  2000000  7919 ns/op  17.68 MB/s  0 B/op  0 allocs/op
func BenchmarkUDPWrite(b *testing.B) {

	size := 140

	udpLocalAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:12345")
	if err != nil {
		b.Fatal("ResolveUDPAddr failed:", err)
	}

	l, err := net.ListenUDP("udp", udpLocalAddr)
	if err != nil {
		b.Fatal("ListenUDP failed:", err)
	}
	defer l.Close()

	sender, err := net.DialUDP("udp", nil, udpLocalAddr)
	if err != nil {
		b.Fatal("DialUDP failed:", err)
	}

	msg := make([]byte, size)

	b.SetBytes(int64(size))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		n, err := sender.Write(msg)
		if err != nil {
			b.Fatal("Write failed", err)
		}
		if n != len(msg) {
			b.Fatalf("Write failed: n=%v (want=%v)", n, len(msg))
		}

	}
}

func BenchmarkMain(mb *testing.B) {

	locker, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		mb.Fatal(err)
	}
	defer locker.Close(1, time.Minute)

	retries := 10
	timeout := time.Minute
	ttl := time.Second
	key := "a"

	mb.Run("LockUnlock",
		func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				lock, _ := locker.Lock(retries, timeout, key, ttl)

				if lock != nil {
					locker.Unlock(retries, timeout, lock)
				}
			}
		})

	mb.Run("LockUpdateUnlock",
		func(b *testing.B) {
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				lock, _ := locker.Lock(retries, timeout, key, ttl)

				for i := 0; i < 8; i++ {
					locker.Update(retries, timeout, lock, ttl)
				}

				if lock != nil {
					locker.Unlock(retries, timeout, lock)
				}
			}
		})
}

func BenchmarkParallel(b *testing.B) {

	locker, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer locker.Close(1, time.Minute)

	b.SetParallelism(10)
	//b.SetBytes(10) // это не байты, а количество запросов к серверу: 1 лок, 1 анлок и 8 апдейтов. итого: 10
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {

		retries := 10
		timeout := time.Minute
		ttl := time.Second
		key := "a"
		i := 0

		for pb.Next() {
			i++

			lock, _ := locker.Lock(retries, timeout, fmt.Sprint(key, "_", i), ttl)

			for i := 0; i < 8; i++ {
				locker.Update(retries, timeout, lock, ttl)
			}

			if lock != nil {
				locker.Unlock(retries, timeout, lock)
			}
		}
	})
}

//
// mock servers code
//
func mockStartServer(serverID uint64, moskServers map[uint64]string, pongFunc callback) (chan bool, error) {

	addr, err := net.ResolveUDPAddr("udp", moskServers[serverID])
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	done := make(chan bool)

	go mockRun(conn, serverID, moskServers, done, pongFunc)

	return done, nil
}

func mockStopServer(done chan bool) {
	close(done)
	time.Sleep(200 * time.Millisecond) // ждём, пока закончится цикл в mock_run()
}

func mockRun(conn *net.UDPConn, serverID uint64, moskServers map[uint64]string, done chan bool, pongFunc callback) {

	var lastReadDeadlineTime time.Time

	for {
		// выходим из цикла, если надо закончить свою работу
		select {
		case <-done:
			conn.Close()
			return
		default:
		}

		b := getByteBuffer()

		// deadline нужен чтобы можно было выйти из цикла и завершить работу
		// Optimization: see https://github.com/golang/go/issues/15133 for details.
		currentTime := time.Now()
		if currentTime.Sub(lastReadDeadlineTime) > 59*time.Second {
			lastReadDeadlineTime = currentTime
			err := conn.SetReadDeadline(currentTime.Add(time.Minute))
			if err != nil {
				warn(err)
			}
		}

		//// deadline нужен чтобы можно было выйти из цикла и завершить работу
		//conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

		n, addr, err := conn.ReadFromUDP(b.buf)
		if err != nil {
			// если произошёл не таймаут, а что-то другое
			if neterr, ok := err.(*net.OpError); !ok || !neterr.Timeout() {
				warn(err)
			}
			putByteBuffer(b)
			continue
		}

		go mockProccessReq(b, n, serverID, moskServers, conn, addr, pongFunc)
	}
}

func mockProccessReq(b *byteBuffer, n int, serverID uint64, moskServers map[uint64]string, conn *net.UDPConn, addr *net.UDPAddr, pongFunc callback) {
	defer putByteBuffer(b)

	pos := 0
	for pos < n {
		pos += protocolHeaderSize
		switch b.buf[pos+0] {
		case code.CONNECT:
			mockOnConnect(conn, addr, serverID, moskServers)
			pos += 1 + 2 + int(binary.LittleEndian.Uint16(b.buf[pos+3:])) + 24

		case code.PING:
			pongFunc(conn, addr, b.buf[pos:])
			pos += 25 + 2 + int(binary.LittleEndian.Uint16(b.buf[pos+25:]))

		case code.TOUCH:
			mockOk(conn, addr, b.buf[pos:])
			pos += 25

		case code.DISCONNECT:
			mockOk(conn, addr, b.buf[pos:])
			pos += 25

		case code.LOCK:
			mockOk(conn, addr, b.buf[pos:])
			pos += 42 + int(b.buf[pos+41]) + 24

		case code.UNLOCK:
			mockOk(conn, addr, b.buf[pos:])
			pos += 58 + int(b.buf[pos+57])

		case code.UPDATE:
			mockOk(conn, addr, b.buf[pos:])
			pos += 66 + int(b.buf[pos+65]) + 24

		default:
			warn("Wrong command", fmt.Sprint(b.buf[pos+0]), pos, n, b.buf)
			return
		}
		pos += protocolTailSize
	}
}

func mockOnConnect(conn *net.UDPConn, addr *net.UDPAddr, serverID uint64, moskServers map[uint64]string) {
	b := getByteBuffer()
	defer putByteBuffer(b)

	// записываем версию
	b.buf[0] = 1

	// записываем приоритет
	b.buf[1] = 128 // выше среднего

	// записываем код команды
	b.buf[2] = 0 // одиночный пакет

	b.buf[protocolHeaderSize+0] = code.OPTIONS

	binary.LittleEndian.PutUint64(b.buf[protocolHeaderSize+1:], serverID)
	binary.LittleEndian.PutUint64(b.buf[protocolHeaderSize+9:], 1)

	binary.LittleEndian.PutUint64(b.buf[protocolHeaderSize+17:], uint64(len(moskServers)))

	serversPos := protocolHeaderSize + 25
	for serverID, serverString := range moskServers {
		// id сервера
		binary.LittleEndian.PutUint64(b.buf[serversPos:], serverID)
		serversPos += 8

		// длина адреса сервера
		b.buf[serversPos] = byte(len(serverString))
		serversPos++

		// адрес сервера
		copy(b.buf[serversPos:], serverString)
		serversPos += len(serverString)
	}

	// записываем длину
	binary.LittleEndian.PutUint64(b.buf[8:], uint64(serversPos+protocolTailSize))

	// записываем контрольную сумму
	chsum := checksum.Checksum(b.buf[:serversPos])
	copy(b.buf[serversPos:], chsum[:])

	serversPos += protocolTailSize

	_, err := conn.WriteToUDP(b.buf[:serversPos], addr)
	if err != nil {
		warn(err)
	}

}

func mockOk(conn *net.UDPConn, addr *net.UDPAddr, reqB []byte) {
	b := getByteBuffer()
	defer putByteBuffer(b)

	// записываем версию
	b.buf[0] = 1

	// записываем приоритет
	b.buf[1] = 128 // выше среднего

	// записываем код команды
	b.buf[2] = 0 // одиночный пакет

	b.buf[32] = code.OK

	copy(b.buf[33:], reqB[1:25])

	// записываем длину
	binary.LittleEndian.PutUint64(b.buf[8:], uint64(protocolHeaderSize+25+protocolTailSize))

	// записываем контрольную сумму
	chsum := checksum.Checksum(b.buf[:protocolHeaderSize+25])
	copy(b.buf[protocolHeaderSize+25:], chsum[:])

	_, err := conn.WriteToUDP(b.buf[:protocolHeaderSize+25+protocolTailSize], addr)
	if err != nil {
		warn(err)
	}
}

// ошибочный пинг-понг - не понгает в ответ на пинг
// нужно для тестирования правильности отработки коннекта
func mockLocked(conn *net.UDPConn, addr *net.UDPAddr, reqB []byte) {
	b := getByteBuffer()
	defer putByteBuffer(b)

	// записываем версию
	b.buf[0] = 1

	// записываем приоритет
	b.buf[1] = 128 // выше среднего

	// записываем код команды
	b.buf[2] = 0 // одиночный пакет

	b.buf[32] = code.LOCKED

	copy(b.buf[33:], reqB[1:25])

	// записываем длину
	binary.LittleEndian.PutUint64(b.buf[8:], uint64(protocolHeaderSize+25+protocolTailSize))

	// записываем контрольную сумму
	chsum := checksum.Checksum(b.buf[:protocolHeaderSize+25])
	copy(b.buf[protocolHeaderSize+25:], chsum[:])

	_, err := conn.WriteToUDP(b.buf[:protocolHeaderSize+25+protocolTailSize], addr)
	if err != nil {
		warn(err)
	}
}

// Keep this lines at the end of file

// go test -v -run=none -benchmem -benchtime="10s" -bench="."

// go test -v -run=none -memprofile mem.out -memprofilerate=1 -benchmem -benchtime="10s" -bench="." github.com/MichaelMonashev/sync/netmutex -x
// --alloc_space, --alloc_objects, --show_bytes
// go tool pprof --alloc_objects netmutex.test.exe mem.out

// go test -v -run=none --blockprofile block.out -benchtime="10s" -benchmem -bench="." github.com/MichaelMonashev/sync/netmutex -x
// go tool pprof --lines netmutex.test.exe block.out

// go test -v -run=none -cpuprofile cpu.out -benchtime="10s" -benchmem -bench="." github.com/MichaelMonashev/sync/netmutex -x
// go tool pprof netmutex.test.exe cpu.out

// go test -v -run=none -mutexprofile mutex.out -benchtime="10s" -benchmem -bench="." github.com/MichaelMonashev/sync/netmutex -x
// go tool pprof netmutex.test.exe mutex.out

// go test -cover -covermode=count -coverprofile=count.out
// go tool cover -html=count.out

// go tool pprof -seconds=1 main.exe http://127.0.0.1:8000/debug/pprof/profile
