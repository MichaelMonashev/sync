// Package checksum calculate hash function value.
package checksum

import (
	"crypto/md5"
)

// Checksum size
const Size = md5.Size

// Checksum returns the MD5 checksum of buf.
func Checksum(buf []byte) [Size]byte {
	//return crc64.Checksum(buf, table)
	return md5.Sum(buf)
}
