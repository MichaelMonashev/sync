// +build go1.7

package netmutex

import (
	"fmt"
	"net"
	"testing"
)

func BenchmarkMain(mb *testing.B) {

	locker, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		mb.Fatal(err)
	}
	defer locker.Close(1, time.Minute)

	retries := 10
	timeout := time.Minute
	ttl := time.Second
	key := "a"

	mb.Run("LockUnlock",
		func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				lock, _ := locker.Lock(retries, timeout, key, ttl)

				if lock != nil {
					locker.Unlock(retries, timeout, lock)
				}
			}
		})

	mb.Run("LockUpdateUnlock",
		func(b *testing.B) {
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				lock, _ := locker.Lock(retries, timeout, key, ttl)

				for i := 0; i < 8; i++ {
					locker.Update(retries, timeout, lock, ttl)
				}

				if lock != nil {
					locker.Unlock(retries, timeout, lock)
				}
			}
		})
}