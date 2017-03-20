// +build go1.8

package netmutex

import (
	"fmt"
	"net"
	"testing"
)

// On FreeBSD 11.0:
//BenchmarkUDP/WriteMany1x140-4    3000000   6057 ns/op   23.11 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers1x140-4   2000000   6238 ns/op   22.44 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig1x140-4  3000000   4708 ns/op   29.73 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/WriteMany10x140-4    300000  45410 ns/op   30.83 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers10x140-4  2000000   8215 ns/op  170.41 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig10x140-4 2000000   6098 ns/op  229.56 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/WriteMany20x140-4    200000  89042 ns/op   31.45 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers20x140-4  1000000  10675 ns/op  262.27 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig20x140-4 2000000   7736 ns/op  361.91 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/WriteMany30x140-4    100000 136303 ns/op   30.81 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers30x140-4  1000000  13641 ns/op  307.89 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig30x140-4 1000000  11616 ns/op  361.56 MB/s   0 B/op  0 allocs/op

//On Ubuntu 16.10:
//BenchmarkUDP/WriteMany1x140-4    2000000   7202 ns/op   19.44 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers1x140-4   2000000   9375 ns/op   14.93 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig1x140-4  2000000   7530 ns/op   18.59 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/WriteMany10x140-4    200000  80140 ns/op   17.47 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers10x140-4  1000000  11054 ns/op  126.64 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig10x140-4 2000000   9860 ns/op  141.99 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/WriteMany20x140-4    100000  40516 ns/op   19.93 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers20x140-4  1000000  14152 ns/op  197.84 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig20x140-4 1000000  11539 ns/op  242.64 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/WriteMany30x140-4    100000 222907 ns/op   18.84 MB/s   0 B/op  0 allocs/op
//BenchmarkUDP/NetBuffers30x140-4  1000000  15546 ns/op  270.16 MB/s  32 B/op  1 allocs/op
//BenchmarkUDP/WriteOneBig30x140-4 1000000  12518 ns/op  335.50 MB/s   0 B/op  0 allocs/op

func BenchmarkUDP(mb *testing.B) {

	size := 140
	benchmarks := []int{1, 10, 20, 30}

	udpLocalAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:12345")
	if err != nil {
		mb.Fatal("ResolveUDPAddr failed:", err)
	}

	l, err := net.ListenUDP("udp", udpLocalAddr)
	if err != nil {
		mb.Fatal("ListenUDP failed:", err)
	}
	defer l.Close()

	sender, err := net.DialUDP("udp", nil, udpLocalAddr)
	if err != nil {
		mb.Fatal("DialUDP failed:", err)
	}

	for _, bm := range benchmarks {
		benchmarkName := fmt.Sprint(bm)

		msg := make([]byte, size)

		mb.Run(fmt.Sprintf("WriteMany%vx%v", benchmarkName, size),
			func(b *testing.B) {

				b.SetBytes(int64(size * bm))
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					for i := 0; i < bm; i++ {
						n, err := sender.Write(msg)
						if err != nil {
							b.Fatal("Write failed", err)
						}
						if n != len(msg) {
							b.Fatalf("Write failed: n=%v (want=%v)", n, len(msg))
						}
					}
				}
			})

		mb.Run(fmt.Sprintf("NetBuffers%vx%v", benchmarkName, size),
			func(b *testing.B) {

				nb0 := make(net.Buffers, bm)
				nb := net.Buffers{}

				b.SetBytes(int64(size * bm))
				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					nb = nb0
					for i := 0; i < bm; i++ {
						nb[i] = msg
					}

					n, err := nb.WriteTo(sender)
					if err != nil {
						b.Fatal("Write failed", err)
					}
					if n != int64(len(msg)*bm) {
						b.Fatalf("Write failed: n=%v (want=%v)", n, len(msg)*bm)
					}
				}
			})

		mb.Run(fmt.Sprintf("WriteOneBig%vx%v", benchmarkName, size),
			func(b *testing.B) {

				bb := make([]byte, 0, size*bm)

				b.SetBytes(int64(size * bm))
				b.ResetTimer()

				for n := 0; n < b.N; n++ {
					for i := 0; i < bm; i++ {
						bb = append(bb, msg...)
					}

					n, err := sender.Write(bb)
					if err != nil {
						b.Fatal("Write failed", err)
					}
					if n != len(msg)*bm {
						b.Fatalf("Write failed: n=%v (want=%v)", n, len(msg)*bm)
					}
					bb = bb[:0]
				}
			})
	}
}
