package main

import (
	"errors"
	"log"
	"net/http"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/database/targetdb"
	"tubes-wbd/backend/http/middleware"
	"tubes-wbd/backend/manageddb"
	"tubes-wbd/backend/migration"
	"tubes-wbd/backend/viewer"
)

func targetErrorMessage(err error) string {
	switch {
	case errors.Is(err, targetdb.ErrAuthFailed):
		return "target database rejected the credentials"
	case errors.Is(err, targetdb.ErrTimeout):
		return "target database connection timed out"
	case errors.Is(err, targetdb.ErrUnreachable):
		return "target database is unreachable"
	case errors.Is(err, targetdb.ErrConnectionFailed):
		return "could not connect to the target database"
	case errors.Is(err, targetdb.ErrManagerClosed):
		return "target database connections are shutting down"
	}
	return ""
}

func writeServiceError(w http.ResponseWriter, err error, operation string) {
	if message := targetErrorMessage(err); message != "" {
		writeAPIError(w, http.StatusBadGateway, message)
		return
	}
	switch {
	case errors.Is(err, access.ErrInsufficientPermission):
		writeAPIError(w, http.StatusForbidden, "insufficient permission on this database")

	case errors.Is(err, manageddb.ErrNotFound),
		errors.Is(err, targetdb.ErrUnknownDatabase):
		writeAPIError(w, http.StatusNotFound, "database not found")
	case errors.Is(err, access.ErrGroupNotFound):
		writeAPIError(w, http.StatusNotFound, "group not found")
	case errors.Is(err, access.ErrPermissionNotFound):
		writeAPIError(w, http.StatusNotFound, "the group has no permission on this database")
	case errors.Is(err, viewer.ErrSchemaNotFound):
		writeAPIError(w, http.StatusNotFound, "schema not found")
	case errors.Is(err, viewer.ErrTableNotFound):
		writeAPIError(w, http.StatusNotFound, "table not found")
	case errors.Is(err, migration.ErrDraftNotFound):
		writeAPIError(w, http.StatusNotFound, "migration draft not found")
	case errors.Is(err, migration.ErrVersionNotFound):
		writeAPIError(w, http.StatusNotFound, "migration version not found")

	case errors.Is(err, manageddb.ErrInvalidInput),
		errors.Is(err, viewer.ErrInvalidInput),
		errors.Is(err, migration.ErrInvalidInput),
		errors.Is(err, migration.ErrTransactionControl),
		errors.Is(err, access.ErrInvalidPermission):
		writeAPIError(w, http.StatusBadRequest, err.Error())

	case errors.Is(err, manageddb.ErrDuplicateName):
		writeAPIError(w, http.StatusConflict, "database name already exists")
	case errors.Is(err, migration.ErrDraftNotEditable),
		errors.Is(err, migration.ErrDraftNotExecutable),
		errors.Is(err, migration.ErrMigrationInProgress):
		writeAPIError(w, http.StatusConflict, err.Error())

	case errors.Is(err, migration.ErrExecutionFailed):
		writeAPIError(w, http.StatusUnprocessableEntity, err.Error())

	default:
		log.Printf("%s failed: %v", operation, err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
	}
}

func currentUserID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	session, ok := middleware.AuthenticatedSessionFromContext(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return 0, false
	}
	return session.UserID, true
}

func positiveID(w http.ResponseWriter, id int64, what string) (uint, bool) {
	if id <= 0 {
		writeAPIError(w, http.StatusBadRequest, what+" id must be positive")
		return 0, false
	}
	return uint(id), true
}

func intValue(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
