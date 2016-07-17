#sync/netmutex - Go client to lock server

Golang client library for reliable distributed lock-servers. Zero memory allocation. Goroutine-safe.
[![Go Report Card](https://goreportcard.com/badge/github.com/MichaelMonashev/sync/netmutex)](https://goreportcard.com/report/github.com/MichaelMonashev/sync/netmutex)

# Goals

 - No bugs.
 - Keep API simple and stable.
 - Low CPU/memory consumption (code inlining, avoid memory allocation, avoid memory copy).
 - Simple, well-documented code. Suitable for easy porting to other languages.
 - No tricks (for compatibility with future Go versions).

## Installing

### Using *go get*

    $ go get github.com/MichaelMonashev/sync/netmutex

After this command *sync/netmutex* is ready to use. Its source will be in:

    $GOPATH/src/github.com/MichaelMonashev/sync/netmutex

## Documentation

Web: [godoc.org/github.com/MichaelMonashev/sync/netmutex](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)

or comand-line:

    $ go doc github.com/MichaelMonashev/sync/netmutex

## Example

5 Steps:
 - connect
 - lock
 - execute critical section
 - unlock
 - close connection

Code:

    package main

    import (
            "fmt"
            "github.com/MichaelMonashev/sync/netmutex"
            "time"
    )

    func main() {
            // Open connection to lock-servers
            nm, err := netmutex.Open([]string{
                    "10.0.0.1:1234",
                    "10.0.0.2:1234",
                    "10.0.0.3:1234",
            }, &netmutex.Options{
                    Timeout: time.Minute, // try to lock()/unlock() during this time
                    TTL:     time.Second, // unlock a key after this duration
            })
            if err != nil {
                    fmt.Println("Connecting error:", err)
                    return
            }

            // Close connection after main() finished
            defer nm.Close()

            key := "some key"

            // Try to lock the key
            lock, err := nm.Lock(key)
            if err != nil {
                    fmt.Println("Error while lock key:", key, "error:", err)
                    return
            }

            fmt.Println("Key:", key, "successful locked!")

            // Do something alone. Sleep, for example. ;-)
            time.Sleep(time.Millisecond)

           // Try to unlock the key
            err = nm.Unlock(lock)
            if err != nil {
                    // No problem. The key will unlock automatically after expiration.
                    fmt.Println("Error while unlock key:", key, "error:", err)
            }
    }

##Test sync/netmutex performance

Start mock-servers:

    $ cd $GOPATH/src/github.com/MichaelMonashev/sync
    $ go run mosk_server1.go &
    $ go run mosk_server2.go &
    $ go run mosk_server3.go &


Start main.go for load testing:

    $ go run main.go
