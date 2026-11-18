package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
)

const aesKeySize = 32 // AES-256

// EncryptAESGCM encrypts plaintext with AES-256-GCM.
// The returned blob is nonce || ciphertext.
func EncryptAESGCM(key, plaintext []byte) ([]byte, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("AES-256 key must be %d bytes", aesKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptAESGCM decrypts a nonce || ciphertext blob produced by EncryptAESGCM.
func DecryptAESGCM(key, blob []byte) ([]byte, error) {
	if len(key) != aesKeySize {
		return nil, fmt.Errorf("AES-256 key must be %d bytes", aesKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(blob) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
