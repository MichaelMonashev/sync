#sync/netmutex - Go client to lock server

Golang client library for reliable distributed lock-servers. Zero memory allocation. Goroutine-safe.
[![Go Report Card](https://goreportcard.com/badge/github.com/MichaelMonashev/sync/netmutex)](https://goreportcard.com/report/github.com/MichaelMonashev/sync/netmutex)

##Test sync/netmutex perfomance

Start mock-servers:

    $ go run mosk_server1.go &
    $ go run mosk_server2.go &
    $ go run mosk_server3.go &


Start main.go for load testing:

    $ go run main.go
