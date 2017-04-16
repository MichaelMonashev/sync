// +build go1.7

package netmutex

import (
	"testing"
	"time"
)

func BenchmarkMain(mb *testing.B) {

	nm, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		mb.Fatal(err)
	}
	defer nm.Close(1, time.Minute)

	retries := 10
	timeout := time.Minute
	ttl := time.Second
	key := "a"

	mb.Run("Lock",
		func(b *testing.B) {

			l := nm.NewLock()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				l.Lock(retries, timeout, key, ttl)
			}
		})

	mb.Run("Unlock",
		func(b *testing.B) {

			l := nm.NewLock()

			l.Lock(retries, timeout, key, ttl)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				l.Unlock(retries, timeout)
			}
		})

	mb.Run("Update",
		func(b *testing.B) {

			l := nm.NewLock()

			l.Lock(retries, timeout, key, ttl)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				l.Update(retries, timeout, ttl)
			}
		})

	mb.Run("LockUnlock",
		func(b *testing.B) {

			l := nm.NewLock()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				l.Lock(retries, timeout, key, ttl)

				l.Unlock(retries, timeout)

			}
		})

	mb.Run("Lock8UpdatesUnlock",
		func(b *testing.B) {
			l := nm.NewLock()

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				l.Lock(retries, timeout, key, ttl)

				for i := 0; i < 8; i++ {
					l.Update(retries, timeout, ttl)
				}

				l.Unlock(retries, timeout)
			}
		})
}
