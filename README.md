# sync/netmutex - Go client to lock server

Low-level high-performance Golang client library for lock server.

[![GoDoc](https://godoc.org/github.com/MichaelMonashev/sync/netmutex?status.svg)](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)
[![Go Report Card](https://goreportcard.com/badge/github.com/MichaelMonashev/sync/netmutex)](https://goreportcard.com/report/github.com/MichaelMonashev/sync/netmutex)
[![Travis CI](https://travis-ci.org/MichaelMonashev/sync.svg)](https://travis-ci.org/MichaelMonashev/sync)
[![AppVeyor](https://ci.appveyor.com/api/projects/status/eit2o9qvcocqyhqd?svg=true)](https://ci.appveyor.com/project/MichaelMonashev/sync)
[![Codeship](https://codeship.com/projects/a1e6d740-389e-0134-23ba-1e19d127eddf/status?branch=master)](https://codeship.com/projects/165999)
[![Coverage](https://coveralls.io/repos/github/MichaelMonashev/sync/badge.svg?branch=master)](https://coveralls.io/github/MichaelMonashev/sync?branch=master)
[![Codecov](https://codecov.io/gh/MichaelMonashev/sync/branch/master/graph/badge.svg)](https://codecov.io/gh/MichaelMonashev/sync)

## Installation

### Using *go get*

	$ go get github.com/MichaelMonashev/sync/netmutex

Its source will be in:

	$GOPATH/src/github.com/MichaelMonashev/sync/netmutex

## Documentation

See: [godoc.org/github.com/MichaelMonashev/sync/netmutex](https://godoc.org/github.com/MichaelMonashev/sync/netmutex)

or run:

	$ godoc github.com/MichaelMonashev/sync/netmutex

## Performance

200000+ locks per second on 10-years old 8-core Linux box.

## Example

Steps:

 - connect to server
 - lock key
 - execute critical section
 - unlock
 - close connection

Code:

	package main

	import (
		"fmt"
		"os"
		"sync/atomic"
		"time"

		"github.com/MichaelMonashev/sync/netmutex"
	)

	func main() {
		retries := 10
		timeout := 60 * time.Second
		addresses := []string{
			"127.0.0.1:15663",
		}

		hostname, err := os.Hostname()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		options := &Options{
			// information about client restart or host remote reboot
			IsolationInfo: fmt.Sprintf("Hostname: %s\nPid: %d", hostname, os.Getpid()),
		}

		// Open connection to a lock-server
		nm, err := Open(retries, timeout, addresses, options)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		lock := &Lock{}
		key := "ObjectID:123456"
		ttl := time.Minute

		// Try to lock key
		err = nm.Lock(retries, timeout, lock, key, ttl)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		var done uint32

		// run heartbeat in background
		go func() {
			heartbeatTimeout := 6 * time.Second // much less than `ttl`
			heartbeatRetries := 1

			for atomic.LoadUint32(&done) == 0 {
				// Try to update lock TTL
				err = nm.Update(heartbeatRetries, timeout, lock, ttl)
				if err == ErrDisconnected || err == ErrWrongTTL || err == ErrNoServers {
					return
				} else if err == ErrIsolated {
					os.Exit(1)
				} else if err == ErrTooMuchRetries {
					continue
				} else if err != nil {
					fmt.Fprintln(os.Stderr, err)
					return
				}

				time.Sleep(heartbeatTimeout)
			}
		}()

		// do something under the lock

		// stop heartbeat
		atomic.StoreUint32(&done, 1)

		// Try to unlock lock
		err = nm.Unlock(retries, timeout, lock)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		// Cloce connection
		err = nm.Close(retries, timeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
	}
