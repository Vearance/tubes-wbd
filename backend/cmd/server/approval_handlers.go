package main

import (
	"net/http"
	httpapi "tubes-wbd/internal/http"
)

// TODO(leader-approval)

func writeNotImplemented(w http.ResponseWriter) {
	writeAPIError(w, http.StatusNotImplemented, "not implemented yet")
}

func (s apiServer) SetAdminGroupLeader(w http.ResponseWriter, _ *http.Request, _ httpapi.GroupId, _ httpapi.SetAdminGroupLeaderParams) {
	writeNotImplemented(w)
}

func (s apiServer) ClearAdminGroupLeader(w http.ResponseWriter, _ *http.Request, _ httpapi.GroupId, _ httpapi.ClearAdminGroupLeaderParams) {
	writeNotImplemented(w)
}

func (s apiServer) GetMigrationExecutionApprovals(w http.ResponseWriter, _ *http.Request, _ httpapi.ExecutionId) {
	writeNotImplemented(w)
}

func (s apiServer) ApproveMigrationExecution(w http.ResponseWriter, _ *http.Request, _ httpapi.ExecutionId, _ httpapi.ApproveMigrationExecutionParams) {
	writeNotImplemented(w)
}

func (s apiServer) RejectMigrationExecution(w http.ResponseWriter, _ *http.Request, _ httpapi.ExecutionId, _ httpapi.RejectMigrationExecutionParams) {
	writeNotImplemented(w)
}

func (s apiServer) EnqueueMigrationExecution(w http.ResponseWriter, _ *http.Request, _ httpapi.ExecutionId, _ httpapi.EnqueueMigrationExecutionParams) {
	writeNotImplemented(w)
}

func (s apiServer) ListPendingMigrationApprovals(w http.ResponseWriter, _ *http.Request) {
	writeNotImplemented(w)
}