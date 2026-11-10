package users

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRepository struct {
	users       map[uint]*User
	next        uint
	createError error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{users: map[uint]*User{}, next: 1}
}

func (r *fakeRepository) FindByUsername(_ context.Context, username string) (*User, error) {
	for _, user := range r.users {
		if user.Username == username {
			clone := *user
			return &clone, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *fakeRepository) FindByID(_ context.Context, userID uint) (*User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, ErrUserNotFound
	}
	clone := *user
	return &clone, nil
}

func (r *fakeRepository) FindAdmin(context.Context) (*User, error) {
	for _, user := range r.users {
		if user.IsAdmin {
			clone := *user
			return &clone, nil
		}
	}
	return nil, ErrUserNotFound
}

func (r *fakeRepository) List(context.Context) ([]User, error) {
	users := make([]User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, *user)
	}
	return users, nil
}

func (r *fakeRepository) Create(_ context.Context, user *User) error {
	if r.createError != nil {
		return r.createError
	}
	if user.ID == 0 {
		user.ID = r.next
		r.next++
	}
	clone := *user
	r.users[user.ID] = &clone
	return nil
}

func (r *fakeRepository) Update(_ context.Context, user *User) error {
	if _, ok := r.users[user.ID]; !ok {
		return ErrUserNotFound
	}
	clone := *user
	r.users[user.ID] = &clone
	return nil
}

type fakeSessionRevoker struct {
	calls  int
	userID uint
}

func (r *fakeSessionRevoker) RevokeAllByUser(_ context.Context, userID uint, _ time.Time) error {
	r.calls++
	r.userID = userID
	return nil
}

type fakePasswordHasher struct{}

func (fakePasswordHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
func (fakePasswordHasher) Compare(hash, password string) error  { return nil }

func newTestService(t *testing.T, repository *fakeRepository) (*Service, *fakeSessionRevoker) {
	t.Helper()
	sessions := &fakeSessionRevoker{}
	service, err := NewService(repository, sessions, fakePasswordHasher{})
	require.NoError(t, err)
	return service, sessions
}

func TestServiceCreatesRegularUser(t *testing.T) {
	repository := newFakeRepository()
	service, _ := newTestService(t, repository)

	user, err := service.CreateUser(context.Background(), " alice ", "secret")
	require.NoError(t, err)
	require.Equal(t, "alice", user.Username)
	require.Equal(t, "hash:secret", user.PasswordHash)
	require.False(t, user.IsAdmin)
}

func TestServiceRejectsDuplicateAndInvalidUser(t *testing.T) {
	repository := newFakeRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username: "alice", PasswordHash: "hash:secret",
	}))
	service, _ := newTestService(t, repository)

	_, err := service.CreateUser(context.Background(), "alice", "secret")
	require.ErrorIs(t, err, ErrUserAlreadyExists)
	_, err = service.CreateUser(context.Background(), "", "secret")
	require.ErrorIs(t, err, ErrInvalidUserInput)
}

func TestServiceDisablesAndEnablesUser(t *testing.T) {
	repository := newFakeRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username: "alice", PasswordHash: "hash:secret",
	}))
	service, sessions := newTestService(t, repository)

	user, err := service.SetDisabled(context.Background(), 1, true)
	require.NoError(t, err)
	require.NotNil(t, user.DisabledAt)
	require.Equal(t, 1, sessions.calls)
	require.Equal(t, uint(1), sessions.userID)

	user, err = service.SetDisabled(context.Background(), 1, false)
	require.NoError(t, err)
	require.Nil(t, user.DisabledAt)
}

func TestServiceCannotDisableAdmin(t *testing.T) {
	repository := newFakeRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username: "admin", PasswordHash: "hash:secret", IsAdmin: true,
	}))
	service, _ := newTestService(t, repository)

	_, err := service.SetDisabled(context.Background(), 1, true)
	require.ErrorIs(t, err, ErrCannotDisableAdmin)
}

func TestServiceUsesUTCClockForDisabledAt(t *testing.T) {
	repository := newFakeRepository()
	require.NoError(t, repository.Create(context.Background(), &User{
		Username: "alice", PasswordHash: "hash:secret",
	}))
	service, _ := newTestService(t, repository)
	service.now = func() time.Time {
		return time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("test", 7*60*60))
	}

	user, err := service.SetDisabled(context.Background(), 1, true)
	require.NoError(t, err)
	require.Equal(t, time.UTC, user.DisabledAt.Location())
}
