// Package checksum считает контрольную сумму.
package checksum

import (
	"hash/crc32"
)

// Size - Checksum size in bytes
const Size = crc32.Size

var table = crc32.MakeTable(crc32.Castagnoli) // Castagnoli имеет ассемблерную инструкцию в SSE4.2, поэтому быстрое.

// Checksum returns the crc32 checksum of buf.
func Checksum(buf []byte) (b [Size]byte) {
	v := crc32.Checksum(buf, table)

	b[0] = byte(v >> 24)
	b[1] = byte(v >> 16)
	b[2] = byte(v >> 8)
	b[3] = byte(v)

	return
}
