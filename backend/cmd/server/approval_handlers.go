package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"tubes-wbd/internal/access"
	httpapi "tubes-wbd/internal/http"
	"tubes-wbd/internal/migration"
	"tubes-wbd/internal/users"
)

const approvalBodyLimit = 16 << 10

func (s apiServer) SetAdminGroupLeader(w http.ResponseWriter, r *http.Request, groupID httpapi.GroupId, _ httpapi.SetAdminGroupLeaderParams) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}
	id, ok := positiveID(w, groupID, "group")
	if !ok {
		return
	}
	var request httpapi.SetGroupLeaderRequest
	if !decodeJSONBodyWithLimit(w, r, &request, approvalBodyLimit) {
		return
	}
	userID, ok := positiveID(w, request.UserId, "user")
	if !ok {
		return
	}

	group, err := s.groupService.SetLeader(r.Context(), id, userID)
	if err != nil {
		writeGroupLeaderError(w, err, "setting group leader")
		return
	}
	writeJSON(w, http.StatusOK, groupResponse(group))
}

func (s apiServer) ClearAdminGroupLeader(w http.ResponseWriter, r *http.Request, groupID httpapi.GroupId, _ httpapi.ClearAdminGroupLeaderParams) {
	if s.groupService == nil {
		writeAPIError(w, http.StatusInternalServerError, "group service unavailable")
		return
	}
	id, ok := positiveID(w, groupID, "group")
	if !ok {
		return
	}
	if err := s.groupService.ClearLeader(r.Context(), id); err != nil {
		writeGroupLeaderError(w, err, "clearing group leader")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeGroupLeaderError(w http.ResponseWriter, err error, operation string) {
	switch {
	case errors.Is(err, access.ErrGroupNotFound),
		errors.Is(err, users.ErrUserNotFound):
		writeAPIError(w, http.StatusNotFound, "group or user not found")
	case errors.Is(err, access.ErrLeaderNotMember),
		errors.Is(err, access.ErrLeaderDisabled):
		writeAPIError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("%s failed: %v", operation, err)
		writeAPIError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (s apiServer) GetMigrationExecutionApprovals(w http.ResponseWriter, r *http.Request, executionID httpapi.ExecutionId) {
	userID, id, ok := executionIDs(w, r, executionID)
	if !ok {
		return
	}
	summary, err := s.migrations.ExecutionApprovals(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "getting migration approvals")
		return
	}
	writeJSON(w, http.StatusOK, approvalSummaryResponse(summary))
}

func (s apiServer) ApproveMigrationExecution(w http.ResponseWriter, r *http.Request, executionID httpapi.ExecutionId, _ httpapi.ApproveMigrationExecutionParams) {
	userID, id, ok := executionIDs(w, r, executionID)
	if !ok {
		return
	}
	var request httpapi.ApproveMigrationRequest
	if !decodeOptionalJSONBody(w, r, &request, approvalBodyLimit) {
		return
	}
	note := ""
	if request.Note != nil {
		note = *request.Note
	}
	summary, err := s.migrations.ApproveExecution(r.Context(), userID, id, note)
	if err != nil {
		writeServiceError(w, err, "approving migration execution")
		return
	}
	writeJSON(w, http.StatusOK, approvalSummaryResponse(summary))
}

func (s apiServer) RejectMigrationExecution(w http.ResponseWriter, r *http.Request, executionID httpapi.ExecutionId, _ httpapi.RejectMigrationExecutionParams) {
	userID, id, ok := executionIDs(w, r, executionID)
	if !ok {
		return
	}
	var request httpapi.RejectMigrationRequest
	if !decodeJSONBodyWithLimit(w, r, &request, approvalBodyLimit) {
		return
	}
	summary, err := s.migrations.RejectExecution(r.Context(), userID, id, request.Note)
	if err != nil {
		writeServiceError(w, err, "rejecting migration execution")
		return
	}
	writeJSON(w, http.StatusOK, approvalSummaryResponse(summary))
}

func (s apiServer) EnqueueMigrationExecution(w http.ResponseWriter, r *http.Request, executionID httpapi.ExecutionId, _ httpapi.EnqueueMigrationExecutionParams) {
	userID, id, ok := executionIDs(w, r, executionID)
	if !ok {
		return
	}
	execution, err := s.migrations.EnqueueExecution(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "queueing migration execution")
		return
	}
	writeJSON(w, http.StatusAccepted, executionResponse(execution))
}

func (s apiServer) ListPendingMigrationApprovals(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return
	}
	executions, err := s.migrations.PendingApprovals(r.Context(), userID)
	if err != nil {
		writeServiceError(w, err, "listing pending migration approvals")
		return
	}
	response := make([]httpapi.MigrationExecution, 0, len(executions))
	for i := range executions {
		response = append(response, executionResponse(&executions[i]))
	}
	writeJSON(w, http.StatusOK, response)
}

func executionIDs(w http.ResponseWriter, r *http.Request, executionID httpapi.ExecutionId) (uint, uint, bool) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return 0, 0, false
	}
	id, ok := positiveID(w, executionID, "execution")
	return userID, id, ok
}

func approvalSummaryResponse(summary *migration.ApprovalSummary) httpapi.MigrationApprovalSummary {
	response := httpapi.MigrationApprovalSummary{
		ExecutionId:     int64(summary.ExecutionID),
		ExecutionStatus: httpapi.MigrationExecutionStatus(summary.ExecutionStatus),
		CanDecide:       summary.CanDecide,
		Approvals:       make([]httpapi.MigrationApproval, 0, len(summary.Approvals)),
	}
	for _, approval := range summary.Approvals {
		response.Approvals = append(response.Approvals, httpapi.MigrationApproval{
			GroupId:           int64(approval.GroupID),
			GroupName:         approval.GroupName,
			LeaderUserId:      uintPointerToInt64(approval.LeaderUserID),
			LeaderUsername:    approval.LeaderUsername,
			Decision:          httpapi.MigrationApprovalDecision(approval.Decision),
			DecidedBy:         uintPointerToInt64(approval.DecidedBy),
			DecidedByUsername: approval.DecidedByUsername,
			DecidedAt:         approval.DecidedAt,
			Note:              optionalString(approval.Note),
		})
	}
	return response
}

func decodeOptionalJSONBody(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
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