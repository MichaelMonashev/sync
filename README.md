# sync/netmutex - Go client to lock server

Golang client library for reliable distributed lock-servers. Zero memory allocation. Goroutine-safe.

[![GoDoc](https://godoc.org/github.com/MichaelMonashev/sync/netmutex?status.svg)](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)
[![Go Report Card](https://goreportcard.com/badge/github.com/MichaelMonashev/sync/netmutex)](https://goreportcard.com/report/github.com/MichaelMonashev/sync/netmutex)
[![Travis CI](https://travis-ci.org/MichaelMonashev/sync.svg)](https://travis-ci.org/MichaelMonashev/sync)
[![AppVeyor](https://ci.appveyor.com/api/projects/status/eit2o9qvcocqyhqd?svg=true)](https://ci.appveyor.com/project/MichaelMonashev/sync)
[![Codeship](https://codeship.com/projects/a1e6d740-389e-0134-23ba-1e19d127eddf/status?branch=master)](https://codeship.com/projects/165999)
[![drone.io](https://drone.io/github.com/MichaelMonashev/sync/status.png)](https://drone.io/github.com/MichaelMonashev/sync/latest)
[![Coverage](https://coveralls.io/repos/github/MichaelMonashev/sync/badge.svg?branch=master)](https://coveralls.io/github/MichaelMonashev/sync?branch=master)
[![Codecov](https://codecov.io/gh/MichaelMonashev/sync/branch/master/graph/badge.svg)](https://codecov.io/gh/MichaelMonashev/sync)

## Goals

 - No bugs;
 - Keep API simple and stable;
 - Low CPU/memory consumption (code inlining, avoid memory allocation, avoid memory copy);
 - Simple, well-documented code. Suitable for easy porting to other languages;
 - No tricks (for compatibility with future Go versions).

## Features

 - IPv6 support;
 - Cross-platform.

## Installing

### Using *go get*

	$ go get github.com/MichaelMonashev/sync/netmutex

After this command *sync/netmutex* is ready to use. Its source will be in:

	$GOPATH/src/github.com/MichaelMonashev/sync/netmutex

## Documentation

See: [godoc.org/github.com/MichaelMonashev/sync/netmutex](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)

or run:

	$ godoc github.com/MichaelMonashev/sync/netmutex

## Example

	lock, err := nm.Lock(key)
	...
	err = nm.Unlock(lock)

All steps:

 - connect
 - lock key
 - execute critical section
 - unlock
 - close connection

Full code:

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

##Performance

	$ cd $GOPATH/src/github.com/MichaelMonashev/sync/netmutex
	$ go test -v -benchmem -benchtime="20s" -bench="."
	...
	BenchmarkLock-8      	500000	 62164 ns/op	153 B/op	 6 allocs/op
	BenchmarkLockUnlock-8	200000	135380 ns/op	211 B/op	10 allocs/op

Golang runtime produce all memory allocations in this benchmarks. I think, it will fixed soon by go team.
