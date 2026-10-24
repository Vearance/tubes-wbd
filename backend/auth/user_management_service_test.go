package auth

import (
	"context"

	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeUserManagementRepository struct {
	users map[uint]*User
	next  uint
}

func newFakeUserManagementRepository() *fakeUserManagementRepository {
	return &fakeUserManagementRepository{
		users: map[uint]*User{},
		next:  1,
	}
}

func (r *fakeUserManagementRepository) FindByUsername(
	_ context.Context,
	username string,
) (*User, error) {
	for _, user := range r.users {
		if user.Username == username {
			clone := *user
			return &clone, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *fakeUserManagementRepository) FindByID(
	_ context.Context,
	userID uint,
) (*User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *user
	return &clone, nil
}

func (r *fakeUserManagementRepository) FindAdmin(context.Context) (*User, error) {
	for _, user := range r.users {
		if user.IsAdmin {
			clone := *user
			return &clone, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *fakeUserManagementRepository) List(context.Context) ([]User, error) {
	users := make([]User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, *user)
	}
	return users, nil
}

func (r *fakeUserManagementRepository) Create(_ context.Context, user *User) error {
	if user.ID == 0 {
		user.ID = r.next
		r.next++
	}
	r.users[user.ID] = cloneUser(user)
	return nil
}

func (r *fakeUserManagementRepository) Update(_ context.Context, user *User) error {
	if _, ok := r.users[user.ID]; !ok {
		return ErrUserNotFound
	}
	r.users[user.ID] = cloneUser(user)
	return nil
}

func TestUserManagementServiceCreatesRegularUser(t *testing.T) {
	repository := newFakeUserManagementRepository()
	service, err := NewUserManagementService(repository, fakePasswordHasher{})
	require.NoError(t, err)

	user, err := service.CreateUser(context.Background(), " alice ", "secret")
	require.NoError(t, err)
	require.Equal(t, "alice", user.Username)
	require.Equal(t, "hash:secret", user.PasswordHash)
	require.False(t, user.IsAdmin)
}

func TestUserManagementServiceRejectsDuplicateAndInvalidUser(t *testing.T) {
	repository := newFakeUserManagementRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username:     "alice",
		PasswordHash: "hash:secret",
	}))
	service, err := NewUserManagementService(repository, fakePasswordHasher{})
	require.NoError(t, err)

	_, err = service.CreateUser(context.Background(), "alice", "secret")
	require.ErrorIs(t, err, ErrUserAlreadyExists)

	_, err = service.CreateUser(context.Background(), "", "secret")
	require.ErrorIs(t, err, ErrInvalidUserInput)
}

func TestUserManagementServiceDisablesAndEnablesUser(t *testing.T) {
	repository := newFakeUserManagementRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username:     "alice",
		PasswordHash: "hash:secret",
	}))
	service, err := NewUserManagementService(repository, fakePasswordHasher{})
	require.NoError(t, err)

	user, err := service.SetDisabled(context.Background(), 1, true)
	require.NoError(t, err)
	require.NotNil(t, user.DisabledAt)

	user, err = service.SetDisabled(context.Background(), 1, false)
	require.NoError(t, err)
	require.Nil(t, user.DisabledAt)
}

func TestUserManagementServiceCannotDisableAdmin(t *testing.T) {
	repository := newFakeUserManagementRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username:     "admin",
		PasswordHash: "hash:secret",
		IsAdmin:      true,
	}))
	service, err := NewUserManagementService(repository, fakePasswordHasher{})
	require.NoError(t, err)

	_, err = service.SetDisabled(context.Background(), 1, true)
	require.ErrorIs(t, err, ErrCannotDisableAdmin)
}

func TestUserManagementServiceUsesUTCClockForDisabledAt(t *testing.T) {
	repository := newFakeUserManagementRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username:     "alice",
		PasswordHash: "hash:secret",
	}))
	service, err := NewUserManagementService(repository, fakePasswordHasher{})
	require.NoError(t, err)
	service.now = func() time.Time {
		return time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("test", 7*60*60))
	}

	user, err := service.SetDisabled(context.Background(), 1, true)
	require.NoError(t, err)
	require.Equal(t, time.UTC, user.DisabledAt.Location())
}
