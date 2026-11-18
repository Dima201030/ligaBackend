package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP-aligned Argon2id defaults (memory in KiB).
const (
	argonTime    uint32 = 2
	argonMemory  uint32 = 19 * 1024 // 19 MiB
	argonThreads uint8  = 1
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// HashPassword returns a PHC-encoded Argon2id hash of password.
// Never log or return the plaintext password or this hash to clients.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return encodePHC(salt, hash, argonTime, argonMemory, argonThreads), nil
}

// VerifyPassword checks password against a PHC Argon2id hash using constant-time compare.
// Do not compare passwords with == or any plaintext equality.
func VerifyPassword(password, encodedHash string) (bool, error) {
	salt, hash, time, memory, threads, err := decodePHC(encodedHash)
	if err != nil {
		return false, err
	}
	computed := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(hash)))
	if subtle.ConstantTimeCompare(computed, hash) == 1 {
		return true, nil
	}
	return false, nil
}

func encodePHC(salt, hash []byte, time, memory uint32, threads uint8) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		memory,
		time,
		threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
}

func decodePHC(encoded string) (salt, hash []byte, time, memory uint32, threads uint8, err error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, fmt.Errorf("unsupported hash format")
	}
	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("hash version: %w", err)
	}
	if version != argon2.Version {
		return nil, nil, 0, 0, 0, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var m, t, p int
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, nil, 0, 0, 0, fmt.Errorf("bad param %q", kv)
		}
		n, convErr := strconv.Atoi(v)
		if convErr != nil {
			return nil, nil, 0, 0, 0, fmt.Errorf("param %s: %w", k, convErr)
		}
		switch k {
		case "m":
			m = n
		case "t":
			t = n
		case "p":
			p = n
		default:
			return nil, nil, 0, 0, 0, fmt.Errorf("unknown param %s", k)
		}
	}
	if m <= 0 || t <= 0 || p <= 0 {
		return nil, nil, 0, 0, 0, fmt.Errorf("invalid argon2 parameters")
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("salt: %w", err)
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("hash: %w", err)
	}
	if len(salt) == 0 || len(hash) == 0 {
		return nil, nil, 0, 0, 0, fmt.Errorf("empty salt or hash")
	}

	return salt, hash, uint32(t), uint32(m), uint8(p), nil
}
