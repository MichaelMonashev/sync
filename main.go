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

	for i := 0; i < 1000; i++ {
		go load()
	}
	load()
}

func load() {
	locker, err := client.Open([]string{
		"127.0.0.1:3001",
		"127.0.0.1:3002",
		"127.0.0.1:3003",
	}, &client.Options{
		Timeout: time.Minute, // попытаться получить блокировку ключа за это время
		TTL:     time.Second, // если получилось, то установить блокировку на это время
	})

	if err != nil {
		panic(err)
	}

	for i := 0; ; i++ {
		if i%100 == 0 {
			fmt.Println(i)
		}

		key := "key_" + fmt.Sprint(i)
		_, err := locker.Lock(key)
		if err != nil {
			fmt.Println("Key:", key, "error:", err)
			continue
		}
	}
	locker.Close()
}
