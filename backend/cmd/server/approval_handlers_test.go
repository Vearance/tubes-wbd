package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"tubes-wbd/internal/access"
	"tubes-wbd/internal/auth"
	httpapi "tubes-wbd/internal/http"
)

func newLeaderEndpointRouter(t *testing.T, isAdmin bool) (http.Handler, *adminEndpointGroups, auth.LoginResult) {
	t.Helper()
	router, users, groups, service := newAdminEndpointRouter(t, isAdmin)
	users.users[3] = &auth.User{ID: 3, Username: "outsider", PasswordHash: "hash:password"}
	groups.groups[1] = &access.Group{ID: 1, Name: "Student Council"}
	groups.memberships[[2]uint{2, 1}] = &access.UserGroup{UserID: 2, GroupID: 1}
	username := "user"
	if isAdmin {
		username = "admin"
	}
	result, err := service.Login(context.Background(), username, "password")
	require.NoError(t, err)
	return router, groups, result
}

func TestGroupLeaderEndpoints(t *testing.T) {
	t.Run("non-admin cannot set a leader", func(t *testing.T) {
		router, groups, session := newLeaderEndpointRouter(t, false)

		response := performAdminRequest(router, http.MethodPut, "/admin/groups/1/leader",
			`{"user_id":2}`, session.Token, session.CSRFToken)

		require.Equal(t, http.StatusForbidden, response.Code)
		require.Nil(t, groups.groups[1].LeaderUserID)
	})

	t.Run("csrf token is required", func(t *testing.T) {
		router, groups, session := newLeaderEndpointRouter(t, true)

		missing := performAdminRequest(router, http.MethodPut, "/admin/groups/1/leader",
			`{"user_id":2}`, session.Token, "")
		require.Equal(t, http.StatusBadRequest, missing.Code)

		wrong := performAdminRequest(router, http.MethodPut, "/admin/groups/1/leader",
			`{"user_id":2}`, session.Token, "not-the-token")
		require.Equal(t, http.StatusForbidden, wrong.Code)
		require.Nil(t, groups.groups[1].LeaderUserID)
	})

	t.Run("admin sets and clears a leader", func(t *testing.T) {
		router, groups, session := newLeaderEndpointRouter(t, true)

		response := performAdminRequest(router, http.MethodPut, "/admin/groups/1/leader",
			`{"user_id":2}`, session.Token, session.CSRFToken)
		require.Equal(t, http.StatusOK, response.Code)
		var group httpapi.Group
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &group))
		require.Equal(t, int64(2), *group.LeaderUserId)

		list := performAdminRequest(router, http.MethodGet, "/admin/groups", "", session.Token, "")
		require.Equal(t, http.StatusOK, list.Code)
		var groupsResponse []httpapi.Group
		require.NoError(t, json.Unmarshal(list.Body.Bytes(), &groupsResponse))
		require.Equal(t, int64(2), *groupsResponse[0].LeaderUserId)

		response = performAdminRequest(router, http.MethodDelete, "/admin/groups/1/leader",
			"", session.Token, session.CSRFToken)
		require.Equal(t, http.StatusNoContent, response.Code)
		require.Nil(t, groups.groups[1].LeaderUserID)
	})

	t.Run("leader must be an existing member", func(t *testing.T) {
		router, groups, session := newLeaderEndpointRouter(t, true)
		cases := []struct {
			name   string
			path   string
			body   string
			status int
		}{
			{"not a member", "/admin/groups/1/leader", `{"user_id":3}`, http.StatusConflict},
			{"unknown user", "/admin/groups/1/leader", `{"user_id":99}`, http.StatusNotFound},
			{"unknown group", "/admin/groups/99/leader", `{"user_id":2}`, http.StatusNotFound},
			{"non-positive user", "/admin/groups/1/leader", `{"user_id":0}`, http.StatusBadRequest},
			{"unknown field", "/admin/groups/1/leader", `{"user_id":2,"extra":true}`, http.StatusBadRequest},
		}
		for _, tc := range cases {
			response := performAdminRequest(router, http.MethodPut, tc.path, tc.body, session.Token, session.CSRFToken)
			require.Equal(t, tc.status, response.Code, tc.name)
		}
		require.Nil(t, groups.groups[1].LeaderUserID)

		response := performAdminRequest(router, http.MethodDelete, "/admin/groups/99/leader",
			"", session.Token, session.CSRFToken)
		require.Equal(t, http.StatusNotFound, response.Code)
	})

	t.Run("leader cannot be removed from the group", func(t *testing.T) {
		router, groups, session := newLeaderEndpointRouter(t, true)
		response := performAdminRequest(router, http.MethodPut, "/admin/groups/1/leader",
			`{"user_id":2}`, session.Token, session.CSRFToken)
		require.Equal(t, http.StatusOK, response.Code)

		response = performAdminRequest(router, http.MethodDelete, "/admin/groups/1/members/2",
			"", session.Token, session.CSRFToken)

		require.Equal(t, http.StatusConflict, response.Code)
		require.Contains(t, groups.memberships, [2]uint{2, 1})
	})
}
