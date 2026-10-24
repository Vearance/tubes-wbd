package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateToken(t *testing.T) {
	token, err := GenerateToken(32)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Len(t, token, 43)

	otherToken, err := GenerateToken(32)
	require.NoError(t, err)
	require.NotEqual(t, token, otherToken)
}

func TestGenerateTokenRejectsInvalidSize(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{name: "zero", size: 0},
		{name: "negative", size: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := GenerateToken(tt.size)
			require.Error(t, err)
			require.Empty(t, token)
		})
	}
}

func TestHashTokenIsDeterministicAndDoesNotExposeToken(t *testing.T) {
	token := "opaque-session-token"

	hash := hashToken(token)
	require.NotEmpty(t, hash)
	require.NotEqual(t, token, hash)
	require.Equal(t, hash, hashToken(token))
}
