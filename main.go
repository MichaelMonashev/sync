package main

import (
	"fmt"
	"time"

	"client"
)

func main() {

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

	locker, err := client.Open([]string{
		"127.0.0.1:3001",
		"127.0.0.1:3002",
		"127.0.0.1:3003",
	}, &client.Options{
		Timeout: time.Minute, // попытаться получить блокировку ключа за это время
		TTL:     time.Second, // если получилось, то установить блокировку на это время
	})

	if err != nil {
		fmt.Println(err)
		return
	}

	//	for i := 1; i < 1000; i++ {
	//		go load(i, locker)
	//	}
	load(0, locker)
}

func load(n int, locker *client.Client) {

	for i := 0; ; i++ {
		if i%10000 == 0 {
			fmt.Println(n, i)
		}

		key := fmt.Sprintf("key_%d_%d", n, i)
		lock, err := locker.Lock(key)
		if err != nil {
			fmt.Println("Error while lock key:", key, "error:", err)
			continue
		}
		err = lock.Unlock()
		if err != nil {
			fmt.Println("Error while lock key:", key, "error:", err)
			continue
		}

		err = locker.ReleaseLock(lock)
		if err != nil {
			fmt.Println("Error while release lock. Key:", key, "error:", err)
			continue
		}
	}
	locker.Close()
}
