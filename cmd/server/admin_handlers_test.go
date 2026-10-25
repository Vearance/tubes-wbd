package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/http/middleware"
)

type adminEndpointUsers struct {
	users map[uint]*auth.User
	next  uint
}

func (r *adminEndpointUsers) FindByUsername(
	_ context.Context,
	username string,
) (*auth.User, error) {
	for _, user := range r.users {
		if user.Username == username {
			return cloneAdminEndpointUser(user), nil
		}
	}

	return nil, auth.ErrUserNotFound
}

func (r *adminEndpointUsers) FindByID(
	_ context.Context,
	userID uint,
) (*auth.User, error) {
	user, ok := r.users[userID]
	if !ok {
		return nil, auth.ErrUserNotFound
	}

	return cloneAdminEndpointUser(user), nil
}

func (r *adminEndpointUsers) FindAdmin(context.Context) (*auth.User, error) {
	for _, user := range r.users {
		if user.IsAdmin {
			return cloneAdminEndpointUser(user), nil
		}
	}

	return nil, auth.ErrUserNotFound
}

func (r *adminEndpointUsers) Create(_ context.Context, user *auth.User) error {
	if user.ID == 0 {
		user.ID = r.next
		r.next++
	}
	r.users[user.ID] = cloneAdminEndpointUser(user)
	return nil
}

func (r *adminEndpointUsers) Update(_ context.Context, user *auth.User) error {
	if _, ok := r.users[user.ID]; !ok {
		return auth.ErrUserNotFound
	}
	r.users[user.ID] = cloneAdminEndpointUser(user)
	return nil
}

func (r *adminEndpointUsers) List(context.Context) ([]auth.User, error) {
	users := make([]auth.User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, *cloneAdminEndpointUser(user))
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].ID < users[j].ID
	})
	return users, nil
}

func cloneAdminEndpointUser(user *auth.User) *auth.User {
	clone := *user
	return &clone
}

type adminEndpointGroups struct {
	groups      map[uint]*access.Group
	memberships map[[2]uint]*access.UserGroup
	permissions map[[2]uint]*access.GroupDatabasePermission
	nextGroup   uint
}

func (r *adminEndpointGroups) CreateGroup(
	_ context.Context,
	group *access.Group,
) error {
	if group.ID == 0 {
		group.ID = r.nextGroup
		r.nextGroup++
	}
	clone := *group
	r.groups[group.ID] = &clone
	return nil
}

func (r *adminEndpointGroups) ListGroups(context.Context) ([]access.Group, error) {
	groups := make([]access.Group, 0, len(r.groups))
	for _, group := range r.groups {
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].ID < groups[j].ID
	})
	return groups, nil
}

func (r *adminEndpointGroups) FindGroupByID(
	_ context.Context,
	groupID uint,
) (*access.Group, error) {
	group, ok := r.groups[groupID]
	if !ok {
		return nil, access.ErrGroupNotFound
	}
	clone := *group
	return &clone, nil
}

func (r *adminEndpointGroups) FindGroupByName(
	_ context.Context,
	name string,
) (*access.Group, error) {
	for _, group := range r.groups {
		if group.Name == name {
			clone := *group
			return &clone, nil
		}
	}
	return nil, access.ErrGroupNotFound
}

func (r *adminEndpointGroups) CreateUserGroup(
	_ context.Context,
	membership *access.UserGroup,
) error {
	key := [2]uint{membership.UserID, membership.GroupID}
	if _, ok := r.memberships[key]; ok {
		return access.ErrMembershipExists
	}
	clone := *membership
	r.memberships[key] = &clone
	return nil
}

func (r *adminEndpointGroups) FindUserGroup(
	_ context.Context,
	userID uint,
	groupID uint,
) (*access.UserGroup, error) {
	membership, ok := r.memberships[[2]uint{userID, groupID}]
	if !ok {
		return nil, access.ErrMembershipNotFound
	}
	clone := *membership
	return &clone, nil
}

func (r *adminEndpointGroups) ListMembers(
	_ context.Context,
	groupID uint,
) ([]auth.User, error) {
	members := make([]auth.User, 0)
	for key := range r.memberships {
		if key[1] == groupID {
			members = append(members, auth.User{
				ID:       key[0],
				Username: "user-" + strconv.FormatUint(uint64(key[0]), 10),
			})
		}
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].ID < members[j].ID
	})
	return members, nil
}

func (r *adminEndpointGroups) DeleteUserGroup(
	_ context.Context,
	userID uint,
	groupID uint,
) error {
	key := [2]uint{userID, groupID}
	if _, ok := r.memberships[key]; !ok {
		return access.ErrMembershipNotFound
	}
	delete(r.memberships, key)
	return nil
}

func (r *adminEndpointGroups) UpsertDatabasePermission(
	_ context.Context,
	permission *access.GroupDatabasePermission,
) error {
	key := [2]uint{permission.GroupID, permission.ManagedDatabaseID}
	clone := *permission
	r.permissions[key] = &clone
	return nil
}

func (r *adminEndpointGroups) DeleteDatabasePermission(
	_ context.Context,
	groupID uint,
	managedDatabaseID uint,
) error {
	key := [2]uint{groupID, managedDatabaseID}
	if _, ok := r.permissions[key]; !ok {
		return access.ErrPermissionNotFound
	}
	delete(r.permissions, key)
	return nil
}

func (r *adminEndpointGroups) ListDatabasePermissions(
	_ context.Context,
	groupID uint,
) ([]access.GroupDatabasePermission, error) {
	permissions := make([]access.GroupDatabasePermission, 0)
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

func TestAdminUserEndpoints(t *testing.T) {
	t.Run("unauthenticated request is rejected", func(t *testing.T) {
		router, _, _, _ := newAdminEndpointRouter(t, false)

		request := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusUnauthorized, response.Code)
	})

	t.Run("non-admin request is forbidden", func(t *testing.T) {
		router, _, _, service := newAdminEndpointRouter(t, false)
		result, err := service.Login(context.Background(), "user", "password")
		require.NoError(t, err)

		request := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
		request.AddCookie(&http.Cookie{
			Name:  middleware.SessionCookieName,
			Value: result.Token,
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusForbidden, response.Code)
	})

	t.Run("admin can create list and disable a user", func(t *testing.T) {
		router, users, _, service := newAdminEndpointRouter(t, true)
		result, err := service.Login(context.Background(), "admin", "password")
		require.NoError(t, err)

		createResponse := performAdminRequest(
			router,
			http.MethodPost,
			"/admin/users",
			`{"username":"alice","password":"secret"}`,
			result.Token,
			result.CSRFToken,
		)
		require.Equal(t, http.StatusCreated, createResponse.Code)
		require.NotContains(t, createResponse.Body.String(), "secret")

		var created httpapi.AdminUser
		require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &created))
		require.Equal(t, "alice", created.Username)
		require.False(t, created.IsAdmin)
		require.Equal(t, "hash:secret", users.users[uint(created.Id)].PasswordHash)

		duplicateResponse := performAdminRequest(
			router,
			http.MethodPost,
			"/admin/users",
			`{"username":"alice","password":"secret"}`,
			result.Token,
			result.CSRFToken,
		)
		require.Equal(t, http.StatusConflict, duplicateResponse.Code)

		listResponse := performAdminRequest(
			router,
			http.MethodGet,
			"/admin/users",
			"",
			result.Token,
			"",
		)
		require.Equal(t, http.StatusOK, listResponse.Code)
		var listed []httpapi.AdminUser
		require.NoError(t, json.Unmarshal(listResponse.Body.Bytes(), &listed))
		require.Len(t, listed, 3)

		updateResponse := performAdminRequest(
			router,
			http.MethodPatch,
			"/admin/users/2",
			`{"disabled":true}`,
			result.Token,
			result.CSRFToken,
		)
		require.Equal(t, http.StatusOK, updateResponse.Code)
		require.True(t, users.users[2].DisabledAt != nil)
	})
}

func TestAdminGroupEndpoints(t *testing.T) {
	t.Run("non-admin cannot manage groups", func(t *testing.T) {
		router, _, _, service := newAdminEndpointRouter(t, false)
		result, err := service.Login(context.Background(), "user", "password")
		require.NoError(t, err)

		response := performAdminRequest(
			router,
			http.MethodPost,
			"/admin/groups",
			`{"name":"developers"}`,
			result.Token,
			result.CSRFToken,
		)
		require.Equal(t, http.StatusForbidden, response.Code)
	})

	router, _, groups, service := newAdminEndpointRouter(t, true)
	result, err := service.Login(context.Background(), "admin", "password")
	require.NoError(t, err)

	invalidResponse := performAdminRequest(
		router,
		http.MethodPost,
		"/admin/groups",
		`{"name":" "}`,
		result.Token,
		result.CSRFToken,
	)
	require.Equal(t, http.StatusBadRequest, invalidResponse.Code)

	createResponse := performAdminRequest(
		router,
		http.MethodPost,
		"/admin/groups",
		`{"name":"developers"}`,
		result.Token,
		result.CSRFToken,
	)
	require.Equal(t, http.StatusCreated, createResponse.Code)

	var created httpapi.Group
	require.NoError(t, json.Unmarshal(createResponse.Body.Bytes(), &created))
	require.Equal(t, "developers", created.Name)

	addPath := "/admin/groups/" + strconv.FormatInt(created.Id, 10) + "/members"
	addResponse := performAdminRequest(
		router,
		http.MethodPost,
		addPath,
		`{"user_id":2}`,
		result.Token,
		result.CSRFToken,
	)
	require.Equal(t, http.StatusNoContent, addResponse.Code)

	duplicateResponse := performAdminRequest(
		router,
		http.MethodPost,
		addPath,
		`{"user_id":2}`,
		result.Token,
		result.CSRFToken,
	)
	require.Equal(t, http.StatusConflict, duplicateResponse.Code)

	membersResponse := performAdminRequest(
		router,
		http.MethodGet,
		addPath,
		"",
		result.Token,
		"",
	)
	require.Equal(t, http.StatusOK, membersResponse.Code)
	var members []httpapi.AdminUser
	require.NoError(t, json.Unmarshal(membersResponse.Body.Bytes(), &members))
	require.Len(t, members, 1)
	require.Equal(t, int64(2), members[0].Id)

	removeResponse := performAdminRequest(
		router,
		http.MethodDelete,
		addPath+"/2",
		"",
		result.Token,
		result.CSRFToken,
	)
	require.Equal(t, http.StatusNoContent, removeResponse.Code)
	require.Empty(t, groups.memberships)
}

func newAdminEndpointRouter(
	t *testing.T,
	isAdmin bool,
) (http.Handler, *adminEndpointUsers, *adminEndpointGroups, *auth.AuthService) {
	t.Helper()

	users := &adminEndpointUsers{
		users: map[uint]*auth.User{
			1: {
				ID:           1,
				Username:     "admin",
				PasswordHash: "hash:password",
				IsAdmin:      true,
			},
			2: {
				ID:           2,
				Username:     "user",
				PasswordHash: "hash:password",
			},
		},
		next: 3,
	}
	sessions := &handlerSessions{}
	service, err := auth.NewAuthService(
		users,
		sessions,
		handlerHasher{},
		auth.ServiceConfig{
			IdleTimeout:         time.Hour,
			AbsoluteLifetime:    8 * time.Hour,
			RotationInterval:    time.Hour,
			RotationGracePeriod: time.Minute,
			Now:                 func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) },
		},
	)
	require.NoError(t, err)

	userManagement, err := auth.NewUserManagementService(users, sessions, handlerHasher{})
	require.NoError(t, err)
	groups := &adminEndpointGroups{
		groups:      map[uint]*access.Group{},
		memberships: map[[2]uint]*access.UserGroup{},
		permissions: map[[2]uint]*access.GroupDatabasePermission{},
		nextGroup:   1,
	}
	groupService, err := access.NewGroupService(users, groups)
	require.NoError(t, err)

	api := apiServer{
		authService:    service,
		userManagement: userManagement,
		groupService:   groupService,
	}
	return httpapi.NewRouter(api, httpapi.RouterConfig{
		Auth:            service,
		AdminAuthorizer: handlerAdminAuthorizer{isAdmin: isAdmin},
	}), users, groups, service
}

func performAdminRequest(
	router http.Handler,
	method string,
	path string,
	body string,
	sessionToken string,
	csrfToken string,
) *httptest.ResponseRecorder {
	var requestBody *strings.Reader
	if body == "" {
		requestBody = strings.NewReader("")
	} else {
		requestBody = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, requestBody)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if sessionToken != "" {
		request.AddCookie(&http.Cookie{
			Name:  middleware.SessionCookieName,
			Value: sessionToken,
		})
	}
	if csrfToken != "" {
		request.Header.Set("X-CSRF-Token", csrfToken)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

var _ auth.UserManagementRepository = (*adminEndpointUsers)(nil)
var _ access.GroupRepository = (*adminEndpointGroups)(nil)
