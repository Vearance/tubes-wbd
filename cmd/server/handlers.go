package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/http/middleware"
	"tubes-wbd/backend/manageddb"
	"tubes-wbd/backend/migration"
	"tubes-wbd/backend/viewer"
)

type apiServer struct {
	authService      *auth.AuthService
	userManagement   *auth.UserManagementService
	groupService     *access.GroupService
	authorization    *access.AuthorizationService
	managedDatabases *manageddb.ManagedDBService
	viewer           *viewer.ViewerService
	migrations       *migration.MigrationService
	cookieOptions    middleware.CookieOptions
}

func (apiServer) GetHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, httpapi.HealthResponse{Status: "ok"})
}

func (apiServer) GetAdminPing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, httpapi.HealthResponse{Status: "ok"})
}

func (s apiServer) PostAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.authService == nil {
		writeAPIError(w, http.StatusInternalServerError, "authentication service unavailable")
		return
	}

	if !requireJSONContentType(w, r) {
		return
	}

	var request httpapi.LoginRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}

	result, err := s.authService.Login(r.Context(), request.Username, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials),
			errors.Is(err, auth.ErrUserDisabled):
			writeAPIError(w, http.StatusUnauthorized, "invalid credentials")
		default:
			log.Printf("login failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	middleware.SetSessionCookie(
		w,
		result.Token,
		result.ExpiresAt,
		s.cookieOptions,
	)
	writeJSON(w, http.StatusOK, httpapi.LoginResponse{
		CsrfToken: result.CSRFToken,
		ExpiresAt: result.ExpiresAt,
		User: httpapi.AuthUser{
			Id:       int64(result.User.ID),
			Username: result.User.Username,
			IsAdmin:  result.User.IsAdmin,
		},
	})
}

func (s apiServer) PostAuthLogout(
	w http.ResponseWriter,
	r *http.Request,
	_ httpapi.PostAuthLogoutParams,
) {
	if s.authService == nil {
		writeAPIError(w, http.StatusInternalServerError, "authentication service unavailable")
		return
	}

	cookie, err := r.Cookie(middleware.SessionCookieName)
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := s.authService.Logout(r.Context(), cookie.Value); err != nil {
		log.Printf("logout failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	middleware.ClearSessionCookie(w, s.cookieOptions)
	w.WriteHeader(http.StatusNoContent)
}

func (s apiServer) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	if s.userManagement == nil {
		writeAPIError(w, http.StatusInternalServerError, "user management service unavailable")
		return
	}

	users, err := s.userManagement.ListUsers(r.Context())
	if err != nil {
		log.Printf("listing admin users failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response := make([]httpapi.AdminUser, 0, len(users))
	for index := range users {
		response = append(response, adminUserResponse(&users[index]))
	}

	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) CreateAdminUser(
	w http.ResponseWriter,
	r *http.Request,
	_ httpapi.CreateAdminUserParams,
) {
	if s.userManagement == nil {
		writeAPIError(w, http.StatusInternalServerError, "user management service unavailable")
		return
	}

	var request httpapi.CreateUserRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}

	user, err := s.userManagement.CreateUser(r.Context(), request.Username, request.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidUserInput):
			writeAPIError(w, http.StatusBadRequest, "username and password are required")
		case errors.Is(err, auth.ErrUserAlreadyExists):
			writeAPIError(w, http.StatusConflict, "username already exists")
		default:
			log.Printf("creating admin user failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusCreated, adminUserResponse(user))
}

func (s apiServer) UpdateAdminUser(
	w http.ResponseWriter,
	r *http.Request,
	userID int64,
	_ httpapi.UpdateAdminUserParams,
) {
	if s.userManagement == nil {
		writeAPIError(w, http.StatusInternalServerError, "user management service unavailable")
		return
	}
	if userID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "user id must be positive")
		return
	}

	var request httpapi.UpdateUserRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}

	user, err := s.userManagement.SetDisabled(r.Context(), uint(userID), request.Disabled)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrCannotDisableAdmin):
			writeAPIError(w, http.StatusForbidden, "platform admin cannot be disabled")
		case errors.Is(err, auth.ErrUserNotFound):
			writeAPIError(w, http.StatusNotFound, "user not found")
		default:
			log.Printf("updating admin user failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusOK, adminUserResponse(user))
}

func (s apiServer) ListAdminGroups(w http.ResponseWriter, r *http.Request) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}

	groups, err := s.groupService.ListGroups(r.Context())
	if err != nil {
		log.Printf("listing admin groups failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response := make([]httpapi.Group, 0, len(groups))
	for index := range groups {
		response = append(response, groupResponse(&groups[index]))
	}

	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) ListAdminGroupMembers(
	w http.ResponseWriter,
	r *http.Request,
	groupID int64,
) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}
	if groupID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "group id must be positive")
		return
	}

	members, err := s.groupService.ListMembers(r.Context(), uint(groupID))
	if err != nil {
		if errors.Is(err, access.ErrGroupNotFound) {
			writeAPIError(w, http.StatusNotFound, "group not found")
			return
		}
		log.Printf("listing group members failed: %v", err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	response := make([]httpapi.AdminUser, 0, len(members))
	for index := range members {
		response = append(response, adminUserResponse(&members[index]))
	}

	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) CreateAdminGroup(
	w http.ResponseWriter,
	r *http.Request,
	_ httpapi.CreateAdminGroupParams,
) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}

	var request httpapi.CreateGroupRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}

	group, err := s.groupService.CreateGroup(r.Context(), request.Name)
	if err != nil {
		switch {
		case errors.Is(err, access.ErrInvalidGroupName):
			writeAPIError(w, http.StatusBadRequest, "group name is required")
		case errors.Is(err, access.ErrGroupAlreadyExists):
			writeAPIError(w, http.StatusConflict, "group name already exists")
		default:
			log.Printf("creating admin group failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	writeJSON(w, http.StatusCreated, groupResponse(group))
}

func (s apiServer) AddAdminGroupMember(
	w http.ResponseWriter,
	r *http.Request,
	groupID int64,
	_ httpapi.AddAdminGroupMemberParams,
) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}
	if groupID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "group id must be positive")
		return
	}

	var request httpapi.AddGroupMemberRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	if request.UserId <= 0 {
		writeAPIError(w, http.StatusBadRequest, "user id must be positive")
		return
	}

	err := s.groupService.AddMember(r.Context(), uint(groupID), uint(request.UserId))
	if err != nil {
		switch {
		case errors.Is(err, access.ErrGroupNotFound),
			errors.Is(err, auth.ErrUserNotFound):
			writeAPIError(w, http.StatusNotFound, "group or user not found")
		case errors.Is(err, access.ErrMembershipExists):
			writeAPIError(w, http.StatusConflict, "membership already exists")
		default:
			log.Printf("adding group member failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s apiServer) RemoveAdminGroupMember(
	w http.ResponseWriter,
	r *http.Request,
	groupID int64,
	userID int64,
	_ httpapi.RemoveAdminGroupMemberParams,
) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}
	if groupID <= 0 || userID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "group and user ids must be positive")
		return
	}

	err := s.groupService.RemoveMember(r.Context(), uint(groupID), uint(userID))
	if err != nil {
		switch {
		case errors.Is(err, access.ErrMembershipNotFound):
			writeAPIError(w, http.StatusNotFound, "membership not found")
		default:
			log.Printf("removing group member failed: %v", err)
			writeAPIError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func adminUserResponse(user *auth.User) httpapi.AdminUser {
	return httpapi.AdminUser{
		Id:       int64(user.ID),
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
		Disabled: user.DisabledAt != nil,
	}
}

func groupResponse(group *access.Group) httpapi.Group {
	return httpapi.Group{
		Id:   int64(group.ID),
		Name: group.Name,
	}
}

func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return false
	}

	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONBodyWithLimit(w, r, target, 1<<20)
}

func decodeJSONBodyWithLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := ensureEndOfJSON(decoder); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return false
	}

	return true
}

func ensureEndOfJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("request body contains multiple json values")
		}
		return err
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("writing json response: %v", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, httpapi.ErrorResponse{Message: message})
}
