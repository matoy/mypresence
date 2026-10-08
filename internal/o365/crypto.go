package o365

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// deriveKey derives a 32-byte key from the secret string using SHA-256.
func deriveKey(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// EncryptToken encrypts plaintext using AES-256-GCM with a random nonce.
// Returns base64 RawURLEncoding ciphertext.
func EncryptToken(plaintext string, secret string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// DecryptToken decrypts base64 RawURLEncoding ciphertext encrypted with EncryptToken.
func DecryptToken(ciphertext string, secret string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	data, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		// Fallback: if not valid base64, return as-is (unencrypted legacy token)
		return ciphertext, nil
	}
	key := deriveKey(secret)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, cipherData := data[:nonceSize], data[nonceSize:]
	plain, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
