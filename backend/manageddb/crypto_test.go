package manageddb

import (
	"bytes"
	"encoding/base64"
	"testing"
	"github.com/stretchr/testify/require"
)

func testKey(fill byte) []byte {
	return bytes.Repeat([]byte{fill}, KeySize)
}

func newTestCipher(t *testing.T, fill byte) *Cipher {
	t.Helper()
	c, err := NewCipher(testKey(fill))
	require.NoError(t, err)
	return c
}

func TestCipherRoundTrip(t *testing.T) {
	c := newTestCipher(t, 1)

	for _, password := range []string{"target_password", "", "pässwörd with spaces & symbols!"} {
		got, err := c.Decrypt(c.Encrypt(password))
		require.NoError(t, err)
		require.Equal(t, password, got)
	}
}

func TestCipherCiphertextHidesPassword(t *testing.T) {
	c := newTestCipher(t, 1)
	password := "target_password"

	require.False(t, bytes.Contains(c.Encrypt(password), []byte(password)))
}

func TestCipherUsesRandomNonce(t *testing.T) {
	c := newTestCipher(t, 1)

	first := c.Encrypt("same-password")
	second := c.Encrypt("same-password")
	require.NotEqual(t, first, second, "same password must not produce the same ciphertext")
}

func TestCipherDecryptWithWrongKey(t *testing.T) {
	ciphertext := newTestCipher(t, 1).Encrypt("target_password")

	_, err := newTestCipher(t, 2).Decrypt(ciphertext)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestCipherDecryptTamperedCiphertext(t *testing.T) {
	c := newTestCipher(t, 1)
	ciphertext := c.Encrypt("target_password")
	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err := c.Decrypt(ciphertext)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestCipherDecryptShortCiphertext(t *testing.T) {
	c := newTestCipher(t, 1)

	for _, ciphertext := range [][]byte{nil, {}, []byte("short")} {
		_, err := c.Decrypt(ciphertext)
		require.ErrorIs(t, err, ErrDecrypt)
	}
}

func TestNewCipherRejectsWrongKeySize(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33} {
		_, err := NewCipher(make([]byte, size))
		require.ErrorIs(t, err, ErrInvalidKey, "size %d", size)
	}
}

func TestParseKey(t *testing.T) {
	key := testKey(7)

	got, err := ParseKey(base64.StdEncoding.EncodeToString(key) + "\n")
	require.NoError(t, err)
	require.Equal(t, key, got)

	tests := map[string]string{
		"empty":      "",
		"not base64": "not-base64!!",
		"too short":  base64.StdEncoding.EncodeToString(make([]byte, 16)),
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseKey(encoded)
			require.ErrorIs(t, err, ErrInvalidKey)
		})
	}
}
