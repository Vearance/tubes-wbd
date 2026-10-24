package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestBcryptHasherHashAndCompare(t *testing.T) {
	hasher := NewBcryptHasher(bcrypt.MinCost)

	hash, err := hasher.Hash("correct horse battery staple")
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	require.NotContains(t, hash, "correct horse battery staple")
	require.NoError(t, hasher.Compare(hash, "correct horse battery staple"))
	require.Error(t, hasher.Compare(hash, "wrong password"))
}

func TestBcryptHasherRejectsEmptyPassword(t *testing.T) {
	hasher := NewBcryptHasher(bcrypt.MinCost)

	hash, err := hasher.Hash("")
	require.Error(t, err)
	require.Empty(t, hash)
}
