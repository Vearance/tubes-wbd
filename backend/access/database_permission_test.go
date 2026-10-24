package access

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/auth"
)

type fakeUsers struct{ auth.UserRepository }

type fakeGroups struct {
	GroupRepository
	groups      map[uint]bool
	permissions map[[2]uint]PermissionLevel
}

func newFakeGroups(groupIDs ...uint) *fakeGroups {
	g := &fakeGroups{groups: map[uint]bool{}, permissions: map[[2]uint]PermissionLevel{}}
	for _, id := range groupIDs {
		g.groups[id] = true
	}
	return g
}

func (g *fakeGroups) FindGroupByID(_ context.Context, groupID uint) (*Group, error) {
	if !g.groups[groupID] {
		return nil, ErrGroupNotFound
	}
	return &Group{ID: groupID}, nil
}

func (g *fakeGroups) UpsertDatabasePermission(_ context.Context, p *GroupDatabasePermission) error {
	g.permissions[[2]uint{p.GroupID, p.ManagedDatabaseID}] = p.Permission
	return nil
}

func newPermissionTestService(t *testing.T, groups *fakeGroups) *GroupService {
	t.Helper()
	svc, err := NewGroupService(fakeUsers{}, groups)
	require.NoError(t, err)
	return svc
}

func TestSetDatabasePermission(t *testing.T) {
	groups := newFakeGroups(1)
	svc := newPermissionTestService(t, groups)

	p, err := svc.SetDatabasePermission(context.Background(), 1, 5, PermissionViewEdit)

	require.NoError(t, err)
	require.Equal(t, uint(1), p.GroupID)
	require.Equal(t, uint(5), p.ManagedDatabaseID)
	require.Equal(t, PermissionViewEdit, p.Permission)
	require.Equal(t, PermissionViewEdit, groups.permissions[[2]uint{1, 5}])
}

func TestSetDatabasePermissionRejectsInvalidLevel(t *testing.T) {
	for _, level := range []PermissionLevel{PermissionNone, 3, 255} {
		groups := newFakeGroups(1)
		svc := newPermissionTestService(t, groups)

		_, err := svc.SetDatabasePermission(context.Background(), 1, 5, level)

		require.ErrorIs(t, err, ErrInvalidPermission, "level %d", level)
		require.Empty(t, groups.permissions)
	}
}

func TestSetDatabasePermissionRequiresExistingGroup(t *testing.T) {
	groups := newFakeGroups()
	svc := newPermissionTestService(t, groups)

	_, err := svc.SetDatabasePermission(context.Background(), 9, 5, PermissionViewOnly)

	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Empty(t, groups.permissions)
}

func TestListDatabasePermissionsRequiresExistingGroup(t *testing.T) {
	svc := newPermissionTestService(t, newFakeGroups())

	_, err := svc.ListDatabasePermissions(context.Background(), 9)

	require.ErrorIs(t, err, ErrGroupNotFound)
}
