package main

import (
	"log"
	"time"

"sync/netmutex"
)

func main() {

	mock_node, err := netmutex.Mock_start_node(3, map[uint64]string{
		1: "127.0.0.1:3001",
		2: "127.0.0.1:3002",
		3: "127.0.0.1:3003",
	})

	if err != nil {
		log.Fatal(err)
	}
	defer netmutex.Mock_stop_node(mock_node)

	log.Println("node 3 successful started")

	time.Sleep(10 * time.Hour)
}
