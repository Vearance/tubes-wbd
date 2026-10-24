package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBootstrapAdminCreatesAdminWhenNoneExists(t *testing.T) {
	users := newFakeUserRepository()

	err := BootstrapAdmin(
		context.Background(),
		users,
		fakePasswordHasher{},
		" admin ",
		"secret",
	)
	require.NoError(t, err)

	admin, err := users.FindAdmin(context.Background())
	require.NoError(t, err)
	require.Equal(t, "admin", admin.Username)
	require.Equal(t, "hash:secret", admin.PasswordHash)
	require.True(t, admin.IsAdmin)
}

func TestBootstrapAdminDoesNotReplaceExistingAdmin(t *testing.T) {
	users := newFakeUserRepository()
	require.NoError(t, users.Create(context.Background(), &User{
		Username:     "existing",
		PasswordHash: "existing-hash",
		IsAdmin:      true,
	}))

	err := BootstrapAdmin(
		context.Background(),
		users,
		fakePasswordHasher{},
		"admin",
		"secret",
	)
	require.NoError(t, err)

	admin, err := users.FindAdmin(context.Background())
	require.NoError(t, err)
	require.Equal(t, "existing", admin.Username)
	require.Equal(t, "existing-hash", admin.PasswordHash)
}

func TestBootstrapAdminRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: "secret"},
		{name: "empty password", username: "admin", password: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := BootstrapAdmin(
				context.Background(),
				newFakeUserRepository(),
				fakePasswordHasher{},
				tt.username,
				tt.password,
			)
			require.Error(t, err)
		})
	}
}

func TestBootstrapAdminPropagatesRepositoryError(t *testing.T) {
	users := newFakeUserRepository()
	users.createError = errors.New("database unavailable")

	err := BootstrapAdmin(
		context.Background(),
		users,
		fakePasswordHasher{},
		"admin",
		"secret",
	)
	require.ErrorContains(t, err, "creating initial admin")
}
