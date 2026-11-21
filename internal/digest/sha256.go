package digest

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hex encodes the SHA-256 digest of text in lowercase hexadecimal.
func SHA256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
