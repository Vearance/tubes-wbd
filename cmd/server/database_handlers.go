package main

import (
	"net/http"

	"tubes-wbd/backend/access"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/manageddb"
)

func (s apiServer) ListAdminDatabases(w http.ResponseWriter, r *http.Request) {
	databases, err := s.managedDatabases.List(r.Context())
	if err != nil {
		writeServiceError(w, err, "listing managed databases")
		return
	}

	response := make([]httpapi.ManagedDatabase, 0, len(databases))
	for index := range databases {
		response = append(response, managedDatabaseResponse(&databases[index]))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) CreateAdminDatabase(
	w http.ResponseWriter,
	r *http.Request,
	_ httpapi.CreateAdminDatabaseParams,
) {
	in, ok := decodeManagedDatabaseInput(w, r)
	if !ok {
		return
	}

	database, err := s.managedDatabases.Create(r.Context(), in)
	if err != nil {
		writeServiceError(w, err, "creating managed database")
		return
	}
	writeJSON(w, http.StatusCreated, managedDatabaseResponse(database))
}

func (s apiServer) TestAdminDatabaseConnection(
	w http.ResponseWriter,
	r *http.Request,
	_ httpapi.TestAdminDatabaseConnectionParams,
) {
	in, ok := decodeManagedDatabaseInput(w, r)
	if !ok {
		return
	}
	writeConnectionTest(w, s.managedDatabases.TestConnection(r.Context(), in))
}

func (s apiServer) GetAdminDatabase(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId) {
	id, ok := positiveID(w, databaseID, "database")
	if !ok {
		return
	}

	database, err := s.managedDatabases.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err, "getting managed database")
		return
	}
	writeJSON(w, http.StatusOK, managedDatabaseResponse(database))
}

func (s apiServer) UpdateAdminDatabase(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	_ httpapi.UpdateAdminDatabaseParams,
) {
	id, ok := positiveID(w, databaseID, "database")
	if !ok {
		return
	}
	in, ok := decodeManagedDatabaseInput(w, r)
	if !ok {
		return
	}

	database, err := s.managedDatabases.Update(r.Context(), id, in)
	if err != nil {
		writeServiceError(w, err, "updating managed database")
		return
	}
	writeJSON(w, http.StatusOK, managedDatabaseResponse(database))
}

func (s apiServer) DeleteAdminDatabase(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	_ httpapi.DeleteAdminDatabaseParams,
) {
	id, ok := positiveID(w, databaseID, "database")
	if !ok {
		return
	}

	if err := s.managedDatabases.Delete(r.Context(), id); err != nil {
		writeServiceError(w, err, "deleting managed database")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s apiServer) TestAdminStoredDatabase(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	_ httpapi.TestAdminStoredDatabaseParams,
) {
	id, ok := positiveID(w, databaseID, "database")
	if !ok {
		return
	}
	writeConnectionTest(w, s.managedDatabases.TestStored(r.Context(), id))
}

func (s apiServer) ListAdminGroupDatabases(w http.ResponseWriter, r *http.Request, groupID httpapi.GroupId) {
	id, ok := positiveID(w, groupID, "group")
	if !ok {
		return
	}

	permissions, err := s.groupService.ListDatabasePermissions(r.Context(), id)
	if err != nil {
		writeServiceError(w, err, "listing group database permissions")
		return
	}

	response := make([]httpapi.DatabasePermission, 0, len(permissions))
	for index := range permissions {
		response = append(response, databasePermissionResponse(&permissions[index]))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) SetAdminGroupDatabase(
	w http.ResponseWriter,
	r *http.Request,
	groupID httpapi.GroupId,
	databaseID httpapi.DatabaseId,
	_ httpapi.SetAdminGroupDatabaseParams,
) {
	gid, ok := positiveID(w, groupID, "group")
	if !ok {
		return
	}
	dbID, ok := positiveID(w, databaseID, "database")
	if !ok {
		return
	}

	var request httpapi.SetDatabasePermissionRequest
	if !decodeJSONBody(w, r, &request) {
		return
	}
	level, ok := permissionLevel(request.Permission)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "permission must be view_only or view_edit")
		return
	}

	permission, err := s.groupService.SetDatabasePermission(r.Context(), gid, dbID, level)
	if err != nil {
		writeServiceError(w, err, "setting group database permission")
		return
	}
	writeJSON(w, http.StatusOK, databasePermissionResponse(permission))
}

func (s apiServer) RemoveAdminGroupDatabase(
	w http.ResponseWriter,
	r *http.Request,
	groupID httpapi.GroupId,
	databaseID httpapi.DatabaseId,
	_ httpapi.RemoveAdminGroupDatabaseParams,
) {
	gid, ok := positiveID(w, groupID, "group")
	if !ok {
		return
	}
	dbID, ok := positiveID(w, databaseID, "database")
	if !ok {
		return
	}

	if err := s.groupService.RemoveDatabasePermission(r.Context(), gid, dbID); err != nil {
		writeServiceError(w, err, "removing group database permission")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s apiServer) ListDatabases(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return
	}

	databases, err := s.managedDatabases.List(r.Context())
	if err != nil {
		writeServiceError(w, err, "listing databases")
		return
	}

	response := make([]httpapi.AccessibleDatabase, 0, len(databases))
	for index := range databases {
		level, err := s.authorization.EffectivePermission(r.Context(), userID, databases[index].ID)
		if err != nil {
			writeServiceError(w, err, "computing database permission")
			return
		}
		permission, ok := permissionResponse(level)
		if !ok {
			continue
		}
		response = append(response, httpapi.AccessibleDatabase{
			Id:         int64(databases[index].ID),
			Name:       databases[index].Name,
			Permission: permission,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeManagedDatabaseInput(w http.ResponseWriter, r *http.Request) (manageddb.Input, bool) {
	var request httpapi.ManagedDatabaseRequest
	if !decodeJSONBody(w, r, &request) {
		return manageddb.Input{}, false
	}
	if request.Port < 1 || request.Port > 65535 {
		writeAPIError(w, http.StatusBadRequest, "port must be between 1 and 65535")
		return manageddb.Input{}, false
	}

	in := manageddb.Input{
		Name:     request.Name,
		Host:     request.Host,
		Port:     uint16(request.Port),
		Username: request.Username,
		DBName:   request.DbName,
	}
	if request.Password != nil {
		in.Password = *request.Password
	}
	if request.SslMode != nil {
		in.SSLMode = *request.SslMode
	}
	return in, true
}

func writeConnectionTest(w http.ResponseWriter, err error) {
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if message := targetErrorMessage(err); message != "" {
		writeAPIError(w, http.StatusUnprocessableEntity, message)
		return
	}
	writeServiceError(w, err, "testing database connection")
}

func managedDatabaseResponse(d *manageddb.ManagedDatabase) httpapi.ManagedDatabase {
	return httpapi.ManagedDatabase{
		Id:        int64(d.ID),
		Name:      d.Name,
		Host:      d.Host,
		Port:      int(d.Port),
		Username:  d.Username,
		DbName:    d.DBName,
		SslMode:   d.SSLMode,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

func databasePermissionResponse(p *access.GroupDatabasePermission) httpapi.DatabasePermission {
	permission, _ := permissionResponse(p.Permission)
	return httpapi.DatabasePermission{
		GroupId:    int64(p.GroupID),
		DatabaseId: int64(p.ManagedDatabaseID),
		Permission: permission,
	}
}

func permissionLevel(p httpapi.PermissionLevel) (access.PermissionLevel, bool) {
	switch p {
	case httpapi.ViewOnly:
		return access.PermissionViewOnly, true
	case httpapi.ViewEdit:
		return access.PermissionViewEdit, true
	}
	return access.PermissionNone, false
}

func permissionResponse(level access.PermissionLevel) (httpapi.PermissionLevel, bool) {
	switch level {
	case access.PermissionViewOnly:
		return httpapi.ViewOnly, true
	case access.PermissionViewEdit:
		return httpapi.ViewEdit, true
	}
	return "", false
}