package main

import (
	"net/http"

	httpapi "tubes-wbd/internal/http"
	"tubes-wbd/internal/migration"
)

const migrationBodyLimit = 3*migration.MaxSQLBytes + 4<<10

func (s apiServer) ListMigrationHistory(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	params httpapi.ListMigrationHistoryParams,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	page, err := s.migrations.History(r.Context(), userID, dbID, intValue(params.Page), intValue(params.PageSize))
	if err != nil {
		writeServiceError(w, err, "listing migration history")
		return
	}

	response := httpapi.MigrationHistoryPage{
		Entries:  make([]httpapi.MigrationHistoryEntry, 0, len(page.Entries)),
		Page:     page.Page,
		PageSize: page.PageSize,
		HasNext:  page.HasNext,
	}
	for _, e := range page.Entries {
		entry := httpapi.MigrationHistoryEntry{
			Version:           e.Version,
			AttemptNumber:     e.AttemptNumber,
			Status:            httpapi.MigrationHistoryEntryStatus(e.Status),
			ErrorCode:         optionalString(e.ErrorCode),
			ErrorMessage:      optionalString(e.ErrorMessage),
			Name:              e.Name,
			ExecutedBy:        int64(e.ExecutedBy),
			ExecutedAt:        e.ExecutedAt,
			DurationMs:        e.Duration.Milliseconds(),
			Active:            new(e.Active),
			ExecutionType:     historyExecutionType(e.Type),
			ExecutionId:       uintPointerToInt64(e.ExecutionID),
			TargetExecutionId: uintPointerToInt64(e.TargetExecutionID),
		}
		response.Entries = append(response.Entries, entry)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) ExplainMigration(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	_ httpapi.ExplainMigrationParams,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	var request httpapi.MigrationExplainRequest
	if !decodeJSONBodyWithLimit(w, r, &request, migrationBodyLimit) {
		return
	}

	result, err := s.migrations.Explain(r.Context(), userID, dbID, request.Sql)
	if err != nil {
		writeServiceError(w, err, "explaining migration SQL")
		return
	}

	response := httpapi.MigrationExplainResponse{
		Statements: make([]httpapi.MigrationExplainStatement, 0, len(result.Statements)),
	}
	for _, statement := range result.Statements {
		response.Statements = append(response.Statements, httpapi.MigrationExplainStatement{
			Plan: statement.Plan,
			Sql:  statement.SQL,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) GetMigrationHistoryEntry(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	version int64,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	entry, err := s.migrations.HistoryEntry(r.Context(), userID, dbID, version)
	if err != nil {
		writeServiceError(w, err, "getting migration history entry")
		return
	}

	executionType := httpapi.MigrationHistoryDetailExecutionType(entry.Type)
	writeJSON(w, http.StatusOK, httpapi.MigrationHistoryDetail{
		Version:            entry.Version,
		AttemptNumber:      entry.AttemptNumber,
		Status:             httpapi.MigrationHistoryDetailStatus(entry.Status),
		ErrorCode:          optionalString(entry.ErrorCode),
		ErrorMessage:       optionalString(entry.ErrorMessage),
		Name:               entry.Name,
		Sql:                entry.SQL,
		DownSql:            optionalString(entry.DownSQL),
		IrreversibleReason: optionalString(entry.IrreversibleReason),
		ExecutedBy:         int64(entry.ExecutedBy),
		ExecutedAt:         entry.ExecutedAt,
		DurationMs:         entry.Duration.Milliseconds(),
		Active:             new(entry.Active),
		ExecutionId:        uintPointerToInt64(nonzeroUintPointer(entry.ExecutionID)),
		ExecutionType:      &executionType,
		TargetExecutionId:  uintPointerToInt64(entry.TargetExecutionID),
	})
}

func (s apiServer) ListMigrationDrafts(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	drafts, err := s.migrations.ListDrafts(r.Context(), userID, dbID)
	if err != nil {
		writeServiceError(w, err, "listing migration drafts")
		return
	}

	response := make([]httpapi.MigrationDraft, 0, len(drafts))
	for index := range drafts {
		response = append(response, draftResponse(&drafts[index]))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) CreateMigrationDraft(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	_ httpapi.CreateMigrationDraftParams,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}
	var request httpapi.MigrationDraftRequest
	if !decodeJSONBodyWithLimit(w, r, &request, migrationBodyLimit) {
		return
	}

	draft, err := s.migrations.CreateDraft(r.Context(), userID, dbID,
		migrationFromRequest(request))
	if err != nil {
		writeServiceError(w, err, "creating migration draft")
		return
	}
	writeJSON(w, http.StatusCreated, draftResponse(draft))
}

func (s apiServer) GetMigrationDraft(w http.ResponseWriter, r *http.Request, draftID httpapi.DraftId) {
	userID, id, ok := draftIDs(w, r, draftID)
	if !ok {
		return
	}

	draft, err := s.migrations.GetDraft(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "getting migration draft")
		return
	}
	writeJSON(w, http.StatusOK, draftResponse(draft))
}

func (s apiServer) UpdateMigrationDraft(
	w http.ResponseWriter,
	r *http.Request,
	draftID httpapi.DraftId,
	_ httpapi.UpdateMigrationDraftParams,
) {
	userID, id, ok := draftIDs(w, r, draftID)
	if !ok {
		return
	}
	var request httpapi.MigrationDraftRequest
	if !decodeJSONBodyWithLimit(w, r, &request, migrationBodyLimit) {
		return
	}

	draft, err := s.migrations.UpdateDraft(r.Context(), userID, id,
		migrationFromRequest(request))
	if err != nil {
		writeServiceError(w, err, "updating migration draft")
		return
	}
	writeJSON(w, http.StatusOK, draftResponse(draft))
}

func (s apiServer) ExecuteMigrationDraft(
	w http.ResponseWriter,
	r *http.Request,
	draftID httpapi.DraftId,
	_ httpapi.ExecuteMigrationDraftParams,
) {
	userID, id, ok := draftIDs(w, r, draftID)
	if !ok {
		return
	}

	execution, err := s.migrations.SubmitDraftForReview(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "submitting migration execution")
		return
	}
	writeJSON(w, http.StatusAccepted, executionResponse(execution))
}

func (s apiServer) SubmitMigrationRollback(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId, _ httpapi.SubmitMigrationRollbackParams) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}
	execution, err := s.migrations.SubmitRollbackForReview(r.Context(), userID, dbID)
	if err != nil {
		writeServiceError(w, err, "submitting migration rollback")
		return
	}
	writeJSON(w, http.StatusAccepted, executionResponse(execution))
}

func (s apiServer) SubmitMigrationForward(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId, _ httpapi.SubmitMigrationForwardParams) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}
	execution, err := s.migrations.SubmitForwardForReview(r.Context(), userID, dbID)
	if err != nil {
		writeServiceError(w, err, "submitting migration forward")
		return
	}
	writeJSON(w, http.StatusAccepted, executionResponse(execution))
}

func (s apiServer) SubmitMigrationCompensation(
	w http.ResponseWriter,
	r *http.Request,
	executionID httpapi.ExecutionId,
	_ httpapi.SubmitMigrationCompensationParams,
) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return
	}
	id, ok := positiveID(w, int64(executionID), "execution")
	if !ok {
		return
	}
	execution, err := s.migrations.SubmitCompensationForReview(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "submitting migration compensation")
		return
	}
	writeJSON(w, http.StatusAccepted, executionResponse(execution))
}

func (s apiServer) ListMigrationExecutions(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}
	executions, err := s.migrations.Executions(r.Context(), userID, dbID)
	if err != nil {
		writeServiceError(w, err, "listing migration executions")
		return
	}
	response := make([]httpapi.MigrationExecution, 0, len(executions))
	for i := range executions {
		response = append(response, executionResponse(&executions[i]))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) GetMigrationExecution(w http.ResponseWriter, r *http.Request, executionID httpapi.ExecutionId) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return
	}
	id, ok := positiveID(w, int64(executionID), "execution")
	if !ok {
		return
	}
	execution, err := s.migrations.Execution(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "getting migration execution")
		return
	}
	writeJSON(w, http.StatusOK, executionResponse(execution))
}

func draftIDs(w http.ResponseWriter, r *http.Request, draftID httpapi.DraftId) (uint, uint, bool) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return 0, 0, false
	}
	id, ok := positiveID(w, draftID, "draft")
	return userID, id, ok
}

func migrationFromRequest(request httpapi.MigrationDraftRequest) migration.Migration {
	m := migration.Migration{Name: request.Name, SQL: request.Sql}
	if request.Batch != nil {
		m.Batch = &migration.BatchDefinition{
			Schema: request.Batch.Schema, Table: request.Batch.Table,
			KeyColumn: request.Batch.KeyColumn, ScopeSQL: request.Batch.ScopeSql,
			BatchSize: request.Batch.BatchSize,
		}
	}
	if request.DownSql != nil {
		m.DownSQL = *request.DownSql
	}
	if request.Irreversible != nil {
		m.Irreversible = *request.Irreversible
	}
	if request.IrreversibleReason != nil {
		m.IrreversibleReason = *request.IrreversibleReason
	}
	return m
}

func draftResponse(d *migration.Draft) httpapi.MigrationDraft {
	return httpapi.MigrationDraft{
		Id:                 int64(d.ID),
		DatabaseId:         int64(d.ManagedDatabaseID),
		Name:               d.Name,
		Sql:                d.SQL,
		DownSql:            optionalString(d.DownSQL),
		Batch:              batchResponse(d.Batch),
		Irreversible:       new(d.Irreversible),
		IrreversibleReason: optionalString(d.IrreversibleReason),
		Status:             httpapi.MigrationDraftStatus(d.Status),
		AppliedVersion:     d.AppliedVersion,
		CreatedBy:          int64(d.CreatedBy),
		UpdatedBy:          int64(d.UpdatedBy),
		CreatedAt:          d.CreatedAt,
		UpdatedAt:          d.UpdatedAt,
	}
}

func batchResponse(batch *migration.BatchDefinition) *httpapi.MigrationBatchDefinition {
	if batch == nil {
		return nil
	}
	return &httpapi.MigrationBatchDefinition{
		Schema: batch.Schema, Table: batch.Table, KeyColumn: batch.KeyColumn,
		ScopeSql: batch.ScopeSQL, BatchSize: batch.BatchSize,
	}
}

func executionResponse(execution *migration.Execution) httpapi.MigrationExecution {
	response := httpapi.MigrationExecution{
		Id:                 int64(execution.ID),
		DatabaseId:         int64(execution.ManagedDatabaseID),
		DraftId:            int64(execution.DraftID),
		Type:               httpapi.MigrationExecutionType(execution.Type),
		Status:             httpapi.MigrationExecutionStatus(execution.Status),
		Compensation:       execution.Compensation,
		Name:               execution.Name,
		Sql:                execution.SQL,
		DownSql:            optionalString(execution.DownSQL),
		Batch:              batchResponse(execution.Batch),
		BatchCount:         new(execution.BatchCount),
		RowsProcessed:      new(execution.RowsProcessed),
		LastCursor:         execution.LastCursor,
		BatchDone:          new(execution.BatchDone),
		Irreversible:       new(execution.Irreversible),
		IrreversibleReason: optionalString(execution.IrreversibleReason),
		ExecutedBy:         int64(execution.ExecutedBy),
		Version:            execution.Version,
		Attempt:            execution.Attempt,
		QueuedAt:           execution.QueuedAt,
		AvailableAt:        execution.AvailableAt,
		StartedAt:          execution.StartedAt,
		FinishedAt:         execution.FinishedAt,
		CreatedAt:          execution.CreatedAt,
		UpdatedAt:          execution.UpdatedAt,
	}
	if execution.TargetExecutionID != nil {
		targetID := int64(*execution.TargetExecutionID)
		response.TargetExecutionId = &targetID
	}
	if execution.ErrorCode != "" {
		response.ErrorCode = &execution.ErrorCode
	}
	if execution.ErrorMessage != "" {
		response.ErrorMessage = &execution.ErrorMessage
	}
	return response
}

func historyExecutionType(value migration.ExecutionType) *httpapi.MigrationHistoryEntryExecutionType {
	mapped := httpapi.MigrationHistoryEntryExecutionType(value)
	return &mapped
}

func nonzeroUintPointer(value uint) *uint {
	if value == 0 {
		return nil
	}
	return &value
}

func uintPointerToInt64(value *uint) *int64 {
	if value == nil {
		return nil
	}
	mapped := int64(*value)
	return &mapped
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
