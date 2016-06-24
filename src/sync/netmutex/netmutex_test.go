package netmutex

import (
	"log"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	//flag.Parse()

	for i := 1; i <= 3; i++ {
		mockNode, err := MockStartNode(uint64(i), map[uint64]string{
			1: "127.0.0.1:3001",
			2: "127.0.0.1:3002",
			3: "127.0.0.1:3003",
		})

		if err != nil {
			log.Fatal(err)
		}
		defer MockStopNode(mockNode)

		log.Println("node", i, "successful started")
	}

	os.Exit(m.Run())
}

func BenchmarkLock(b *testing.B) {

	locker, err := Open([]string{
		"127.0.0.1:3001",
		"127.0.0.1:3002",
		"127.0.0.1:3003",
	}, nil)

	if err != nil {
		log.Fatal(err)
	}

	defer locker.Close()

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		key := "a"

		locker.Lock(key)
	}
}
func BenchmarkLockUnlock(b *testing.B) {

	locker, err := Open([]string{
		"127.0.0.1:3001",
		"127.0.0.1:3002",
		"127.0.0.1:3003",
	}, nil)

	if err != nil {
		log.Fatal(err)
	}
	defer locker.Close()

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		key := "a"

		lock, _ := locker.Lock(key)
		if lock != nil {
			locker.Unlock(lock)
		}
	}
}

// go test -memprofile mem.out -memprofilerate=1 -benchmem -benchtime="10s" -bench="." netmutex -x
// go tool pprof netmutex.test.exe mem.out

// go test -cpuprofile cpu.out -benchmem -benchtime="10s" -bench="." netmutex -x
// go tool pprof netmutex.test.exe cpu.out
