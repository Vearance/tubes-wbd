package access

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/auth"
)

type fakeGroupRepository struct {
	groups      map[uint]*Group
	memberships map[[2]uint]*UserGroup
	permissions map[[2]uint]*GroupDatabasePermission
	nextGroup   uint
}

func newFakeGroupRepository() *fakeGroupRepository {
	return &fakeGroupRepository{
		groups:      map[uint]*Group{},
		memberships: map[[2]uint]*UserGroup{},
		permissions: map[[2]uint]*GroupDatabasePermission{},
		nextGroup:   1,
	}
}

func (r *fakeGroupRepository) CreateGroup(_ context.Context, group *Group) error {
	if group.ID == 0 {
		group.ID = r.nextGroup
		r.nextGroup++
	}
	r.groups[group.ID] = &Group{ID: group.ID, Name: group.Name}
	return nil
}

func (r *fakeGroupRepository) ListGroups(context.Context) ([]Group, error) {
	groups := make([]Group, 0, len(r.groups))
	for _, group := range r.groups {
		groups = append(groups, *group)
	}
	return groups, nil
}

func (r *fakeGroupRepository) FindGroupByID(
	_ context.Context,
	groupID uint,
) (*Group, error) {
	group, ok := r.groups[groupID]
	if !ok {
		return nil, ErrGroupNotFound
	}
	clone := *group
	return &clone, nil
}

func (r *fakeGroupRepository) FindGroupByName(
	_ context.Context,
	name string,
) (*Group, error) {
	for _, group := range r.groups {
		if group.Name == name {
			clone := *group
			return &clone, nil
		}
	}
	return nil, ErrGroupNotFound
}

func (r *fakeGroupRepository) CreateUserGroup(
	_ context.Context,
	membership *UserGroup,
) error {
	key := [2]uint{membership.UserID, membership.GroupID}
	r.memberships[key] = &UserGroup{
		ID:      membership.ID,
		UserID:  membership.UserID,
		GroupID: membership.GroupID,
	}
	return nil
}

func (r *fakeGroupRepository) FindUserGroup(
	_ context.Context,
	userID uint,
	groupID uint,
) (*UserGroup, error) {
	membership, ok := r.memberships[[2]uint{userID, groupID}]
	if !ok {
		return nil, ErrMembershipNotFound
	}
	clone := *membership
	return &clone, nil
}

func (r *fakeGroupRepository) ListMembers(
	_ context.Context,
	groupID uint,
) ([]auth.User, error) {
	members := make([]auth.User, 0)
	for key := range r.memberships {
		if key[1] == groupID {
			members = append(members, auth.User{ID: key[0]})
		}
	}
	return members, nil
}

func (r *fakeGroupRepository) DeleteUserGroup(
	_ context.Context,
	userID uint,
	groupID uint,
) error {
	key := [2]uint{userID, groupID}
	if _, ok := r.memberships[key]; !ok {
		return ErrMembershipNotFound
	}
	delete(r.memberships, key)
	return nil
}

func (r *fakeGroupRepository) UpsertDatabasePermission(
	_ context.Context,
	permission *GroupDatabasePermission,
) error {
	key := [2]uint{permission.GroupID, permission.ManagedDatabaseID}
	clone := *permission
	r.permissions[key] = &clone
	return nil
}

func (r *fakeGroupRepository) DeleteDatabasePermission(
	_ context.Context,
	groupID uint,
	managedDatabaseID uint,
) error {
	key := [2]uint{groupID, managedDatabaseID}
	if _, ok := r.permissions[key]; !ok {
		return ErrPermissionNotFound
	}
	delete(r.permissions, key)
	return nil
}

func (r *fakeGroupRepository) ListDatabasePermissions(
	_ context.Context,
	groupID uint,
) ([]GroupDatabasePermission, error) {
	permissions := make([]GroupDatabasePermission, 0)
	for key, permission := range r.permissions {
		if key[0] == groupID {
			permissions = append(permissions, *permission)
		}
	}
	sort.Slice(permissions, func(i, j int) bool {
		return permissions[i].ManagedDatabaseID < permissions[j].ManagedDatabaseID
	})
	return permissions, nil
}

func TestGroupServiceCreatesGroupsAndManagesMembership(t *testing.T) {
	users := &fakeUserRepository{users: map[uint]*auth.User{
		1: {ID: 1, Username: "alice"},
	}}
	groups := newFakeGroupRepository()
	service, err := NewGroupService(users, groups)
	require.NoError(t, err)

	group, err := service.CreateGroup(context.Background(), " developers ")
	require.NoError(t, err)
	require.Equal(t, "developers", group.Name)

	require.NoError(t, service.AddMember(context.Background(), group.ID, 1))

	members, err := service.ListMembers(context.Background(), group.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, uint(1), members[0].ID)

	require.ErrorIs(t, service.AddMember(context.Background(), group.ID, 1), ErrMembershipExists)
	require.NoError(t, service.RemoveMember(context.Background(), group.ID, 1))
	require.ErrorIs(t, service.RemoveMember(context.Background(), group.ID, 1), ErrMembershipNotFound)
}

func TestGroupServiceRejectsInvalidAndDuplicateGroups(t *testing.T) {
	users := &fakeUserRepository{users: map[uint]*auth.User{}}
	groups := newFakeGroupRepository()
	service, err := NewGroupService(users, groups)
	require.NoError(t, err)

	_, err = service.CreateGroup(context.Background(), " ")
	require.ErrorIs(t, err, ErrInvalidGroupName)

	_, err = service.CreateGroup(context.Background(), "developers")
	require.NoError(t, err)
	_, err = service.CreateGroup(context.Background(), "developers")
	require.ErrorIs(t, err, ErrGroupAlreadyExists)
}

func TestGroupServiceRejectsMissingGroupOrUser(t *testing.T) {
	users := &fakeUserRepository{users: map[uint]*auth.User{}}
	groups := newFakeGroupRepository()
	service, err := NewGroupService(users, groups)
	require.NoError(t, err)

	_, err = service.CreateGroup(context.Background(), "developers")
	require.NoError(t, err)

	require.ErrorIs(t, service.AddMember(context.Background(), 99, 1), ErrGroupNotFound)
	require.ErrorIs(t, service.AddMember(context.Background(), 1, 99), auth.ErrUserNotFound)
}
