// Package checksum считает контрольную сумму.
package checksum

import (
	//"hash/crc64"
	"crypto/md5"
)

const Size = md5.Size

// Checksum returns the MD5 checksum of buf.
func Checksum(buf []byte) [Size]byte {
	//return crc64.Checksum(buf, table)
	return md5.Sum(buf)
}
