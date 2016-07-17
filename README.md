#sync/netmutex - Go client to lock server

Golang client library for reliable distributed lock-servers. Zero memory allocation. Goroutine-safe.
[![Go Report Card](https://goreportcard.com/badge/github.com/MichaelMonashev/sync/netmutex)](https://goreportcard.com/report/github.com/MichaelMonashev/sync/netmutex)


## Installing

### Using *go get*

    $ go get github.com/MichaelMonashev/sync/netmutex

After this command *sync/netmutex* is ready to use. Its source will be in:

    $GOPATH/src/github.com/MichaelMonashev/sync/netmutex

## Example

    package main

    import (
            "fmt"
            "github.com/MichaelMonashev/sync/netmutex"
            "time"
    )

    func main() {
            nm, err := netmutex.Open([]string{
                    "10.0.0.1:1234",
                    "10.0.0.2:1234",
                    "10.0.0.3:1234",
            }, &netmutex.Options{
                    Timeout: time.Minute,
                    TTL:     time.Second,
            })

            if err != nil {
                    fmt.Println("Connecting error:", err)
                    return
            }
            defer nm.Close()

            key := "some key"
            lock, err := nm.Lock(key)
            if err != nil {
                    fmt.Println("Error while lock key:", key, "error:", err)
                    return
            }
            err = nm.Unlock(lock)
            if err != nil {
                    fmt.Println("Error while unlock key:", key, "error:", err)
            }
    }

##Test sync/netmutex perfomance

Start mock-servers:

    $ go run mosk_server1.go &
    $ go run mosk_server2.go &
    $ go run mosk_server3.go &


Start main.go for load testing:

    $ go run main.go
