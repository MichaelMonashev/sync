// +build go1.7

package netmutex

import (
	"testing"
	"time"
)

func BenchmarkMain(mb *testing.B) {

	conn, err := Open(10, time.Minute, addresses, nil)
	if err != nil {
		mb.Fatal(err)
	}
	defer conn.Close(1, time.Minute)

	retries := 10
	timeout := time.Minute
	ttl := time.Second
	key := "a"

	mb.Run("Lock",
		func(b *testing.B) {

			m := conn.NewMutex()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				m.Lock(retries, timeout, key, ttl)
			}
		})

	mb.Run("Unlock",
		func(b *testing.B) {

			m := conn.NewMutex()

			m.Lock(retries, timeout, key, ttl)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				m.Unlock(retries, timeout)
			}
		})

	mb.Run("Update",
		func(b *testing.B) {

			m := conn.NewMutex()

			m.Lock(retries, timeout, key, ttl)

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				m.Update(retries, timeout, ttl)
			}
		})

	mb.Run("LockUnlock",
		func(b *testing.B) {

			m := conn.NewMutex()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				m.Lock(retries, timeout, key, ttl)

				m.Unlock(retries, timeout)

			}
		})

	mb.Run("Lock8UpdatesUnlock",
		func(b *testing.B) {
			m := conn.NewMutex()

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				m.Lock(retries, timeout, key, ttl)

				for i := 0; i < 8; i++ {
					m.Update(retries, timeout, ttl)
				}

				m.Unlock(retries, timeout)
			}
		})
}
