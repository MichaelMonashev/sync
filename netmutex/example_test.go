package netmutex

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"
)

func Example() {

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

	// Open connection to a Taooka lock server (http://taooka.com/)
	conn, err := Open(retries, timeout, addresses, options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	mutex := conn.NewMutex()
	key := "ObjectID:123456"
	ttl := time.Minute

	// Try to lock key
	fenceID, err := mutex.Lock(retries, timeout, key, ttl)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	_ = fenceID // fo prevent error: "fenceID declared and not used"

	var done uint32

	// run heartbeat in background
	go func() {
		heartbeatTimeout := 6 * time.Second // much less than `ttl`
		heartbeatRetries := 1

		for atomic.LoadUint32(&done) == 0 {
			// Try to update lock TTL
			err = mutex.Update(heartbeatRetries, timeout, ttl)
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

	// DO SOMETHING UNDER THE LOCK

	// stop heartbeat
	atomic.StoreUint32(&done, 1)

	// Try to unlock key
	err = mutex.Unlock(retries, timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}

	// Cloce connection
	err = conn.Close(retries, timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
}
