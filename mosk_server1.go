package main

import (
	"log"
	"time"

	"client"
)

func main() {

	err := client.Mock_start_node(1, map[uint64]string{
		1: "127.0.0.1:3001",
		2: "127.0.0.1:3002",
		3: "127.0.0.1:3003",
	})

	if err != nil {
		log.Fatal(err)
	}
	log.Println("node 1 successful started")
	time.Sleep(10 * time.Hour)
}
