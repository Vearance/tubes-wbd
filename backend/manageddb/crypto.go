package manageddb

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// key len aes-256.
const KeySize = 32

var (
	ErrInvalidKey = errors.New("manageddb: encryption key must be 32 bytes")
	ErrDecrypt    = errors.New("manageddb: cannot decrypt password")
)

type Cipher struct {
	aead cipher.AEAD
}

func ParseKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: not valid base64", ErrInvalidKey)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKey, len(key))
	}
	return key, nil
}

func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKey, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) Encrypt(password string) []byte {
	return c.aead.Seal(nil, nil, []byte(password), nil)
}

func (c *Cipher) Decrypt(ciphertext []byte) (string, error) {
	password, err := c.aead.Open(nil, nil, ciphertext, nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(password), nil
}
