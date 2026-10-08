package datapackage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Version derives a package version from its inputs as
// prefix + "-" + the first 8 hex chars of sha256 over the inputs. Each input
// is preceded by its byte length as a big-endian uint64, so ("ab", "c") and
// ("a", "bc") differ. The Python producers compute the same value, so the
// encoding must not change.
func Version(prefix string, inputs ...string) string {
	h := sha256.New()
	var length [8]byte
	for _, in := range inputs {
		binary.BigEndian.PutUint64(length[:], uint64(len(in)))
		h.Write(length[:])
		h.Write([]byte(in))
	}
	return prefix + "-" + hex.EncodeToString(h.Sum(nil))[:8]
}
