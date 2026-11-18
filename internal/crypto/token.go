package crypto

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashToken returns the SHA-256 digest of a session (or similar) token.
// Only the digest should be stored in the database.
func HashToken(token []byte) [32]byte {
	return sha256.Sum256(token)
}

// HashTokenHex is HashToken encoded as lowercase hex.
func HashTokenHex(token []byte) string {
	sum := HashToken(token)
	return hex.EncodeToString(sum[:])
}
