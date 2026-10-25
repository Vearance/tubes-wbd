package frontend

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/manageddb"
)

func (h *Handler) adminRoot(w http.ResponseWriter, r *http.Request, _ pageSession) {
	h.redirect(w, r, "/admin/users")
}

func (h *Handler) adminUsers(w http.ResponseWriter, r *http.Request, session pageSession) {
	users, err := h.deps.UserManagement.ListUsers(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load users")
		return
	}

	data := h.baseAdmin(session, "users")
	data.Title = "Users · BScheme Admin"
	data.Users = make([]adminUserView, 0, len(users))
	groupNames := h.userGroupNames(r, users)
	for index := range users {
		user := users[index]
		status, class := "Active", "status-success"
		if user.DisabledAt != nil {
			status, class = "Disabled", "status-danger"
		}
		data.Users = append(data.Users, adminUserView{
			ID:          user.ID,
			Username:    user.Username,
			Role:        userRole(user.IsAdmin),
			Status:      status,
			StatusClass: class,
			GroupNames:  groupNames[user.ID],
		})
	}
	if r.URL.Query().Has("saved") {
		data.Notice = "User saved."
	}
	h.render(w, "admin-users", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminUserNew(w http.ResponseWriter, _ *http.Request, session pageSession) {
	data := h.baseAdmin(session, "users")
	data.Title = "New user · BScheme Admin"
	data.FormRole = "User"
	data.FormStatus = "Active"
	h.render(w, "admin-user-form", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminUserEdit(w http.ResponseWriter, r *http.Request, session pageSession) {
	username := strings.TrimSpace(r.PathValue("username"))
	user, err := h.deps.Users.FindByUsername(r.Context(), username)
	if err != nil || user == nil {
		h.renderError(w, http.StatusNotFound, "User not found")
		return
	}

	status := "Active"
	if user.DisabledAt != nil {
		status = "Disabled"
	}
	data := h.baseAdmin(session, "users")
	data.Title = "Edit user · BScheme Admin"
	data.Editing = true
	data.FormUsername = user.Username
	data.FormRole = userRole(user.IsAdmin)
	data.FormStatus = status
	h.render(w, "admin-user-form", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminUserSave(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	editing := r.FormValue("editing") == "1"
	status := r.FormValue("status")
	if username == "" {
		h.renderAdminUserFormError(w, session, editing, username, "Username is required.", http.StatusUnprocessableEntity)
		return
	}

	if editing {
		user, err := h.deps.Users.FindByUsername(r.Context(), username)
		if err != nil || user == nil {
			h.renderError(w, http.StatusNotFound, "User not found")
			return
		}
		_, err = h.deps.UserManagement.SetDisabled(r.Context(), user.ID, status == "Disabled")
		if err != nil {
			message := "Unable to update user."
			if errors.Is(err, auth.ErrCannotDisableAdmin) {
				message = "Platform Admin cannot be disabled."
			}
			h.renderAdminUserFormError(w, session, true, username, message, http.StatusUnprocessableEntity)
			return
		}
	} else {
		if password == "" {
			h.renderAdminUserFormError(w, session, false, username, "Password is required.", http.StatusUnprocessableEntity)
			return
		}
		user, err := h.deps.UserManagement.CreateUser(r.Context(), username, password)
		if err != nil {
			message := "Unable to create user."
			switch {
			case errors.Is(err, auth.ErrInvalidUserInput):
				message = "Username and password are required."
			case errors.Is(err, auth.ErrUserAlreadyExists):
				message = "Username already exists."
			}
			h.renderAdminUserFormError(w, session, false, username, message, http.StatusUnprocessableEntity)
			return
		}
		if status == "Disabled" {
			if _, err := h.deps.UserManagement.SetDisabled(r.Context(), user.ID, true); err != nil {
				h.renderError(w, http.StatusInternalServerError, "Unable to update user")
				return
			}
		}
	}
	h.redirect(w, r, "/admin/users?saved=1")
}

func (h *Handler) renderAdminUserFormError(w http.ResponseWriter, session pageSession, editing bool, username, message string, status int) {
	data := h.baseAdmin(session, "users")
	data.Title = "User · BScheme Admin"
	data.Editing = editing
	data.FormUsername = username
	data.FormRole = "User"
	data.FormStatus = "Active"
	data.FormError = message
	h.render(w, "admin-user-form", "admin-layout", data, status)
}

func (h *Handler) adminGroups(w http.ResponseWriter, r *http.Request, session pageSession) {
	groups, err := h.deps.Groups.ListGroups(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load groups")
		return
	}
	data := h.baseAdmin(session, "groups")
	data.Title = "Groups · BScheme Admin"
	data.Groups = make([]adminGroupView, 0, len(groups))
	for index := range groups {
		members, memberErr := h.deps.Groups.ListMembers(r.Context(), groups[index].ID)
		permissions, permissionErr := h.deps.Groups.ListDatabasePermissions(r.Context(), groups[index].ID)
		if memberErr != nil || permissionErr != nil {
			h.renderError(w, http.StatusInternalServerError, "Unable to load group details")
			return
		}
		data.Groups = append(data.Groups, adminGroupView{
			ID:          groups[index].ID,
			Name:        groups[index].Name,
			MemberCount: len(members),
			AccessCount: len(permissions),
		})
	}
	if r.URL.Query().Has("saved") {
		data.Notice = "Group changes saved."
	}
	h.render(w, "admin-groups", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminGroupNew(w http.ResponseWriter, r *http.Request, session pageSession) {
	data, ok := h.groupFormData(w, r, session, 0)
	if !ok {
		return
	}
	data.Title = "New group · BScheme Admin"
	h.render(w, "admin-group", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminGroupDetail(w http.ResponseWriter, r *http.Request, session pageSession) {
	groupID, err := h.parseID(r, "groupID")
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid group ID")
		return
	}
	data, ok := h.groupFormData(w, r, session, groupID)
	if !ok {
		return
	}
	data.Title = data.Group.Name + " · BScheme Admin"
	if r.URL.Query().Has("saved") {
		data.Notice = "Group changes saved."
	}
	h.render(w, "admin-group", "admin-layout", data, http.StatusOK)
}

func (h *Handler) groupFormData(w http.ResponseWriter, r *http.Request, session pageSession, groupID uint) (adminPageData, bool) {
	data := h.baseAdmin(session, "groups")
	users, err := h.deps.UserManagement.ListUsers(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load users")
		return data, false
	}
	databases, err := h.deps.ManagedDatabase.List(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load databases")
		return data, false
	}
	for index := range users {
		if users[index].IsAdmin {
			continue
		}
		data.AvailableUsers = append(data.AvailableUsers, adminUserView{
			ID:       users[index].ID,
			Username: users[index].Username,
			Role:     userRole(users[index].IsAdmin),
		})
	}
	for index := range databases {
		data.AvailableDatabases = append(data.AvailableDatabases, h.databaseView(&databases[index], access.PermissionNone))
	}
	if groupID == 0 {
		return data, true
	}

	group, err := h.findGroup(r, groupID)
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Group not found")
		return data, false
	}
	members, err := h.deps.Groups.ListMembers(r.Context(), groupID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load group members")
		return data, false
	}
	permissions, err := h.deps.Groups.ListDatabasePermissions(r.Context(), groupID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load group permissions")
		return data, false
	}
	databaseNames := make(map[uint]string, len(databases))
	for index := range databases {
		databaseNames[databases[index].ID] = databases[index].Name
	}
	data.GroupID = groupID
	data.Group.Name = group.Name
	for index := range members {
		if members[index].IsAdmin {
			continue
		}
		data.Group.Members = append(data.Group.Members, adminMemberView{
			ID:       members[index].ID,
			Username: members[index].Username,
			Role:     userRole(members[index].IsAdmin),
		})
	}
	for index := range permissions {
		data.Group.Access = append(data.Group.Access, adminAccessView{
			ID:         permissions[index].ID,
			Database:   databaseNames[permissions[index].ManagedDatabaseID],
			DatabaseID: permissions[index].ManagedDatabaseID,
			Permission: permissionLabel(permissions[index].Permission),
		})
	}
	return data, true
}

func (h *Handler) adminGroupSave(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	groupID, _ := strconv.ParseUint(r.FormValue("group_id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		data, _ := h.groupFormData(w, r, session, uint(groupID))
		data.Title = "Group · BScheme Admin"
		data.FormError = "Group name is required."
		h.render(w, "admin-group", "admin-layout", data, http.StatusUnprocessableEntity)
		return
	}

	if groupID == 0 {
		group, err := h.deps.Groups.CreateGroup(r.Context(), name)
		if err != nil {
			data, _ := h.groupFormData(w, r, session, 0)
			data.Title, data.FormError = "New group · BScheme Admin", "Unable to create group."
			if errors.Is(err, access.ErrGroupAlreadyExists) {
				data.FormError = "Group name already exists."
			}
			h.render(w, "admin-group", "admin-layout", data, http.StatusUnprocessableEntity)
			return
		}
		groupID = uint64(group.ID)
	} else {
		group, err := h.findGroup(r, uint(groupID))
		if err != nil {
			h.renderError(w, http.StatusNotFound, "Group not found")
			return
		}
		if name != group.Name {
			// GroupService currently has no rename operation. Keep the persisted name.
			name = group.Name
		}
	}

	if err := h.syncGroupMembers(r, uint(groupID)); err != nil {
		if errors.Is(err, access.ErrAdminMembershipNotAllowed) {
			h.renderError(w, http.StatusUnprocessableEntity, "Platform Admin cannot be a group member")
			return
		}
		h.renderError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := h.syncGroupPermissions(r, uint(groupID)); err != nil {
		h.renderError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.redirect(w, r, fmt.Sprintf("/admin/groups/%d?saved=1", groupID))
}

func (h *Handler) syncGroupMembers(r *http.Request, groupID uint) error {
	desired := make(map[uint]bool)
	for _, username := range r.Form["member"] {
		user, err := h.deps.Users.FindByUsername(r.Context(), strings.TrimSpace(username))
		if err != nil || user == nil {
			return fmt.Errorf("user %q not found", username)
		}
		desired[user.ID] = true
	}
	current, err := h.deps.Groups.ListMembers(r.Context(), groupID)
	if err != nil {
		return err
	}
	for index := range current {
		if !desired[current[index].ID] {
			if err := h.deps.Groups.RemoveMember(r.Context(), groupID, current[index].ID); err != nil {
				return err
			}
		}
		delete(desired, current[index].ID)
	}
	for userID := range desired {
		if err := h.deps.Groups.AddMember(r.Context(), groupID, userID); err != nil && !errors.Is(err, access.ErrMembershipExists) {
			return err
		}
	}
	return nil
}

func (h *Handler) syncGroupPermissions(r *http.Request, groupID uint) error {
	databases, err := h.deps.ManagedDatabase.List(r.Context())
	if err != nil {
		return err
	}
	byName := make(map[string]uint, len(databases))
	for index := range databases {
		byName[databases[index].Name] = databases[index].ID
	}
	desired := make(map[uint]access.PermissionLevel)
	names := r.Form["access_database"]
	levels := r.Form["access_permission"]
	for index, name := range names {
		dbID, ok := byName[strings.TrimSpace(name)]
		if !ok {
			return fmt.Errorf("database %q not found", name)
		}
		level := access.PermissionViewOnly
		if index < len(levels) && levels[index] == "View + Edit" {
			level = access.PermissionViewEdit
		}
		desired[dbID] = level
	}
	current, err := h.deps.Groups.ListDatabasePermissions(r.Context(), groupID)
	if err != nil {
		return err
	}
	for index := range current {
		level, ok := desired[current[index].ManagedDatabaseID]
		if !ok {
			if err := h.deps.Groups.RemoveDatabasePermission(r.Context(), groupID, current[index].ManagedDatabaseID); err != nil {
				return err
			}
			continue
		}
		if _, err := h.deps.Groups.SetDatabasePermission(r.Context(), groupID, current[index].ManagedDatabaseID, level); err != nil {
			return err
		}
		delete(desired, current[index].ManagedDatabaseID)
	}
	for dbID, level := range desired {
		if _, err := h.deps.Groups.SetDatabasePermission(r.Context(), groupID, dbID, level); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) adminDatabases(w http.ResponseWriter, r *http.Request, session pageSession) {
	databases, err := h.deps.ManagedDatabase.List(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load databases")
		return
	}
	data := h.baseAdmin(session, "databases")
	data.Title = "Database connections · BScheme Admin"
	for index := range databases {
		data.Connections = append(data.Connections, h.databaseView(&databases[index], access.PermissionNone))
	}
	if r.URL.Query().Has("saved") {
		data.Notice = "Database connection saved."
	}
	h.render(w, "admin-databases", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminDatabaseNew(w http.ResponseWriter, _ *http.Request, session pageSession) {
	data := h.baseAdmin(session, "databases")
	data.Title = "Connect database · BScheme Admin"
	data.Connection = databaseView{Port: 5432, SSLMode: "Prefer"}
	h.render(w, "admin-database-form", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminDatabaseEdit(w http.ResponseWriter, r *http.Request, session pageSession) {
	id, err := h.parseID(r, "databaseID")
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid database ID")
		return
	}
	db, err := h.deps.ManagedDatabase.Get(r.Context(), id)
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Database not found")
		return
	}
	data := h.baseAdmin(session, "databases")
	data.Title = "Edit connection · BScheme Admin"
	data.Editing = true
	data.Connection = h.databaseView(db, access.PermissionNone)
	h.render(w, "admin-database-form", "admin-layout", data, http.StatusOK)
}

func (h *Handler) adminDatabaseSave(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	id, _ := strconv.ParseUint(r.FormValue("connection_id"), 10, 64)
	port, _ := strconv.ParseUint(r.FormValue("port"), 10, 16)
	input := manageddb.Input{
		Name:     strings.TrimSpace(r.FormValue("display_name")),
		Host:     strings.TrimSpace(r.FormValue("host")),
		Port:     uint16(port),
		Username: strings.TrimSpace(r.FormValue("username")),
		Password: r.FormValue("password"),
		DBName:   strings.TrimSpace(r.FormValue("database")),
		SSLMode:  strings.ToLower(strings.TrimSpace(r.FormValue("ssl_mode"))),
	}
	var err error
	if id == 0 {
		_, err = h.deps.ManagedDatabase.Create(r.Context(), input)
	} else {
		_, err = h.deps.ManagedDatabase.Update(r.Context(), uint(id), input)
	}
	if err != nil {
		data := h.baseAdmin(session, "databases")
		data.Title, data.Editing, data.FormError = "Database connection · BScheme Admin", id != 0, err.Error()
		data.Connection = databaseView{ID: uint(id), Name: input.Name, Host: input.Host, Port: input.Port, Database: input.DBName, Username: input.Username, SSLMode: displaySSLMode(input.SSLMode)}
		h.render(w, "admin-database-form", "admin-layout", data, http.StatusUnprocessableEntity)
		return
	}
	h.redirect(w, r, "/admin/databases?saved=1")
}

func (h *Handler) adminDatabaseTest(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid form"})
		return
	}

	id, _ := strconv.ParseUint(r.FormValue("connection_id"), 10, 64)
	input := manageddb.Input{
		Name:     strings.TrimSpace(r.FormValue("display_name")),
		Host:     strings.TrimSpace(r.FormValue("host")),
		Username: strings.TrimSpace(r.FormValue("username")),
		Password: r.FormValue("password"),
		DBName:   strings.TrimSpace(r.FormValue("database")),
		SSLMode:  strings.ToLower(strings.TrimSpace(r.FormValue("ssl_mode"))),
	}
	port, err := strconv.ParseUint(r.FormValue("port"), 10, 16)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "port must be between 1 and 65535"})
		return
	}
	input.Port = uint16(port)

	if id != 0 && input.Password == "" {
		err = h.deps.ManagedDatabase.TestStored(r.Context(), uint(id))
	} else {
		err = h.deps.ManagedDatabase.TestConnection(r.Context(), input)
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "connection test failed"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) findGroup(r *http.Request, groupID uint) (*access.Group, error) {
	groups, err := h.deps.Groups.ListGroups(r.Context())
	if err != nil {
		return nil, err
	}
	for index := range groups {
		if groups[index].ID == groupID {
			return &groups[index], nil
		}
	}
	return nil, access.ErrGroupNotFound
}

func (h *Handler) userGroupNames(r *http.Request, users []auth.User) map[uint][]adminUserGroupView {
	result := make(map[uint][]adminUserGroupView, len(users))
	groups, err := h.deps.Groups.ListGroups(r.Context())
	if err != nil {
		return result
	}
	for index := range groups {
		members, err := h.deps.Groups.ListMembers(r.Context(), groups[index].ID)
		if err != nil {
			continue
		}
		for memberIndex := range members {
			result[members[memberIndex].ID] = append(result[members[memberIndex].ID], adminUserGroupView{ID: groups[index].ID, Name: groups[index].Name})
		}
	}
	return result
}

func userRole(isAdmin bool) string {
	if isAdmin {
		return "Platform Admin"
	}
	return "User"
}

func permissionLabel(level access.PermissionLevel) string {
	if level == access.PermissionViewEdit {
		return "View + Edit"
	}
	return "View Only"
}
