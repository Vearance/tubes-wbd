package access

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/auth"
)

type fakeUserRepository struct {
	users map[uint]*auth.User
}

func (r *fakeUserRepository) FindByUsername(context.Context, string) (*auth.User, error) {
	return nil, auth.ErrUserNotFound
}

func (r *fakeUserRepository) FindByID(
	_ context.Context,
	userID uint,
) (*auth.User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, auth.ErrUserNotFound
	}

	clone := *user
	return &clone, nil
}

func (r *fakeUserRepository) FindAdmin(context.Context) (*auth.User, error) {
	for _, user := range r.users {
		if user.IsAdmin {
			clone := *user
			return &clone, nil
		}
	}

	return nil, auth.ErrUserNotFound
}

func (r *fakeUserRepository) Create(context.Context, *auth.User) error {
	return errors.New("not implemented")
}

func (r *fakeUserRepository) Update(context.Context, *auth.User) error {
	return errors.New("not implemented")
}

type fakeAccessRepository struct {
	groupIDs    []uint
	permissions []GroupDatabasePermission
	err         error
}

func (r *fakeAccessRepository) FindGroupIDsByUserID(
	context.Context,
	uint,
) ([]uint, error) {
	if r.err != nil {
		return nil, r.err
	}

	return append([]uint(nil), r.groupIDs...), nil
}

func (r *fakeAccessRepository) FindPermissionsByGroupIDs(
	context.Context,
	[]uint,
	uint,
) ([]GroupDatabasePermission, error) {
	if r.err != nil {
		return nil, r.err
	}

	return append([]GroupDatabasePermission(nil), r.permissions...), nil
}

func TestAuthorizationServiceEffectivePermissionUsesMaximum(t *testing.T) {
	service, err := NewAuthorizationService(
		&fakeUserRepository{users: map[uint]*auth.User{}},
		&fakeAccessRepository{
			groupIDs: []uint{1, 2},
			permissions: []GroupDatabasePermission{
				{GroupID: 1, ManagedDatabaseID: 10, Permission: PermissionViewOnly},
				{GroupID: 2, ManagedDatabaseID: 10, Permission: PermissionViewEdit},
			},
		},
	)
	require.NoError(t, err)

	permission, err := service.EffectivePermission(context.Background(), 7, 10)
	require.NoError(t, err)
	require.Equal(t, PermissionViewEdit, permission)
	require.NoError(t, service.RequireView(context.Background(), 7, 10))
	require.NoError(t, service.RequireEdit(context.Background(), 7, 10))
}

func TestAuthorizationServiceRejectsInsufficientPermission(t *testing.T) {
	service, err := NewAuthorizationService(
		&fakeUserRepository{users: map[uint]*auth.User{}},
		&fakeAccessRepository{
			permissions: []GroupDatabasePermission{
				{Permission: PermissionViewOnly},
			},
		},
	)
	require.NoError(t, err)

	require.NoError(t, service.RequireView(context.Background(), 7, 10))
	require.ErrorIs(t, service.RequireEdit(context.Background(), 7, 10), ErrInsufficientPermission)
}

func TestAuthorizationServiceIsAdmin(t *testing.T) {
	service, err := NewAuthorizationService(
		&fakeUserRepository{users: map[uint]*auth.User{
			1: {ID: 1, IsAdmin: true},
			2: {ID: 2, IsAdmin: false},
		}},
		&fakeAccessRepository{},
	)
	require.NoError(t, err)

	isAdmin, err := service.IsAdmin(context.Background(), 1)
	require.NoError(t, err)
	require.True(t, isAdmin)

	isAdmin, err = service.IsAdmin(context.Background(), 2)
	require.NoError(t, err)
	require.False(t, isAdmin)
}

func TestAuthorizationServiceRejectsInvalidPermission(t *testing.T) {
	service, err := NewAuthorizationService(
		&fakeUserRepository{users: map[uint]*auth.User{}},
		&fakeAccessRepository{
			permissions: []GroupDatabasePermission{
				{Permission: PermissionLevel(3)},
			},
		},
	)
	require.NoError(t, err)

	_, err = service.EffectivePermission(context.Background(), 7, 10)
	require.ErrorIs(t, err, ErrInvalidPermission)
}
