package access

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/auth"
)

func newLeaderTestService(t *testing.T) (*GroupService, *fakeGroupRepository) {
	t.Helper()
	disabledAt := time.Now()
	users := &fakeUserRepository{users: map[uint]*auth.User{
		1: {ID: 1, Username: "rudolf"},
		2: {ID: 2, Username: "airgroove"},
		3: {ID: 3, Username: "retired", DisabledAt: &disabledAt},
	}}
	groups := newFakeGroupRepository()
	groups.groups[1] = &Group{ID: 1, Name: "Student Council"}
	groups.memberships[[2]uint{1, 1}] = &UserGroup{UserID: 1, GroupID: 1}
	groups.memberships[[2]uint{2, 1}] = &UserGroup{UserID: 2, GroupID: 1}
	groups.memberships[[2]uint{3, 1}] = &UserGroup{UserID: 3, GroupID: 1}
	service, err := NewGroupService(users, groups)
	require.NoError(t, err)
	return service, groups
}

func TestGroupServiceSetsAndReplacesLeader(t *testing.T) {
	service, groups := newLeaderTestService(t)
	ctx := context.Background()

	group, err := service.SetLeader(ctx, 1, 1)
	require.NoError(t, err)
	require.Equal(t, uint(1), *group.LeaderUserID)

	group, err = service.SetLeader(ctx, 1, 2)
	require.NoError(t, err)
	require.Equal(t, uint(2), *group.LeaderUserID)
	require.Equal(t, uint(2), *groups.groups[1].LeaderUserID)
}

func TestGroupServiceRejectsInvalidLeader(t *testing.T) {
	service, groups := newLeaderTestService(t)
	ctx := context.Background()
	delete(groups.memberships, [2]uint{2, 1})

	_, err := service.SetLeader(ctx, 1, 2)
	require.ErrorIs(t, err, ErrLeaderNotMember)

	_, err = service.SetLeader(ctx, 1, 3)
	require.ErrorIs(t, err, ErrLeaderDisabled)

	_, err = service.SetLeader(ctx, 1, 99)
	require.ErrorIs(t, err, auth.ErrUserNotFound)

	_, err = service.SetLeader(ctx, 99, 1)
	require.ErrorIs(t, err, ErrGroupNotFound)

	require.Nil(t, groups.groups[1].LeaderUserID)
}

func TestGroupServiceClearsLeader(t *testing.T) {
	service, groups := newLeaderTestService(t)
	ctx := context.Background()
	_, err := service.SetLeader(ctx, 1, 1)
	require.NoError(t, err)

	require.NoError(t, service.ClearLeader(ctx, 1))
	require.Nil(t, groups.groups[1].LeaderUserID)
	require.ErrorIs(t, service.ClearLeader(ctx, 99), ErrGroupNotFound)
}

func TestGroupServiceKeepsLeaderMembership(t *testing.T) {
	service, groups := newLeaderTestService(t)
	ctx := context.Background()
	_, err := service.SetLeader(ctx, 1, 1)
	require.NoError(t, err)

	require.ErrorIs(t, service.RemoveMember(ctx, 1, 1), ErrMemberIsLeader)
	require.Contains(t, groups.memberships, [2]uint{1, 1})

	require.NoError(t, service.RemoveMember(ctx, 1, 2))
	require.NoError(t, service.ClearLeader(ctx, 1))
	require.NoError(t, service.RemoveMember(ctx, 1, 1))
}
