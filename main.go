package main

import (
	"fmt"
	"time"

	//	"net/http"
	//	_ "net/http/pprof"
	//	"runtime"

	"sync/netmutex"
)

func main() {

	//	runtime.MemProfileRate = 1
	//	runtime.SetBlockProfileRate(1)
	//
	//	go func() {
	//		fmt.Println(http.ListenAndServe("127.0.0.1:8080", nil))
	//	}()

	//for i := 1; i <= 3; i++ {
	//	err := client.Mock_start_node(uint64(i), map[uint64]string{
	//		1: "127.0.0.1:3001",
	//		2: "127.0.0.1:3002",
	//		3: "127.0.0.1:3003",
	//	})
	//
	//	if err != nil {
	//		log.Fatal(err)
	//	}
	//}

	nm, err := netmutex.Open([]string{
		"127.0.0.1:3001",
		"127.0.0.1:3002",
		"127.0.0.1:3003",
	}, &netmutex.Options{
		Timeout: time.Minute, // попытаться получить блокировку ключа за это время
		TTL:     time.Second, // если получилось, то установить блокировку на это время
	})

	if err != nil {
		fmt.Println(err)
		return
	}
	defer nm.Close()

	for i := 1; i < 100; i++ {
		go load(i, nm)
	}
	load(0, nm)

}

func load(n int, nm *netmutex.NetMutex) {
	for i := 0; ; i++ {
		if i%1000 == 0 {
			fmt.Println(n, i)
		}

		key := fmt.Sprintf("key_%d_%d", n, i)
		lock, err := nm.Lock(key)
		if err != nil {
			fmt.Println("Error while lock key:", key, "error:", err)
			continue
		}
		err = nm.Unlock(lock)
		if err != nil {
			fmt.Println("Error while unlock key:", key, "error:", err)
			continue
		}
	}
}
