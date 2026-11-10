package users

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBootstrapAdminCreatesAdminWhenNoneExists(t *testing.T) {
	repository := newFakeRepository()

	err := BootstrapAdmin(context.Background(), repository, fakePasswordHasher{}, " admin ", "secret")
	require.NoError(t, err)

	admin, err := repository.FindAdmin(context.Background())
	require.NoError(t, err)
	require.Equal(t, "admin", admin.Username)
	require.Equal(t, "hash:secret", admin.PasswordHash)
	require.True(t, admin.IsAdmin)
}

func TestBootstrapAdminDoesNotReplaceExistingAdmin(t *testing.T) {
	repository := newFakeRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username: "existing", PasswordHash: "existing-hash", IsAdmin: true,
	}))

	err := BootstrapAdmin(context.Background(), repository, fakePasswordHasher{}, "admin", "secret")
	require.NoError(t, err)

	admin, err := repository.FindAdmin(context.Background())
	require.NoError(t, err)
	require.Equal(t, "existing", admin.Username)
	require.Equal(t, "existing-hash", admin.PasswordHash)
}

func TestBootstrapAdminRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name     string
		username string
		password string
	}{
		{name: "empty username", username: "", password: "secret"},
		{name: "empty password", username: "admin", password: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := BootstrapAdmin(context.Background(), newFakeRepository(), fakePasswordHasher{}, tt.username, tt.password)
			require.Error(t, err)
		})
	}
}

func TestBootstrapAdminPropagatesRepositoryError(t *testing.T) {
	repository := newFakeRepository()
	repository.createError = errors.New("database unavailable")

	err := BootstrapAdmin(context.Background(), repository, fakePasswordHasher{}, "admin", "secret")
	require.ErrorContains(t, err, "creating initial admin")
}
