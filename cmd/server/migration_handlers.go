package main

import (
	"errors"
	"net/http"

	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/migration"
)

const migrationBodyLimit = 2*migration.MaxSQLBytes + 4<<10

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
		response.Entries = append(response.Entries, httpapi.MigrationHistoryEntry{
			Version:    e.Version,
			Name:       e.Name,
			ExecutedBy: int64(e.ExecutedBy),
			ExecutedAt: e.ExecutedAt,
			DurationMs: e.Duration.Milliseconds(),
		})
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

	writeJSON(w, http.StatusOK, httpapi.MigrationHistoryDetail{
		Version:    entry.Version,
		Name:       entry.Name,
		Sql:        entry.SQL,
		ExecutedBy: int64(entry.ExecutedBy),
		ExecutedAt: entry.ExecutedAt,
		DurationMs: entry.Duration.Milliseconds(),
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
		migration.Migration{Name: request.Name, SQL: request.Sql})
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
		migration.Migration{Name: request.Name, SQL: request.Sql})
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

	run, err := s.migrations.ExecuteDraft(r.Context(), userID, id)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, runResponse(run))
	case run != nil && (errors.Is(err, migration.ErrExecutionFailed) || errors.Is(err, migration.ErrTransactionControl)):
		writeJSON(w, http.StatusUnprocessableEntity, runResponse(run))
	default:
		writeServiceError(w, err, "executing migration draft")
	}
}

func (s apiServer) ListMigrationRuns(w http.ResponseWriter, r *http.Request, draftID httpapi.DraftId) {
	userID, id, ok := draftIDs(w, r, draftID)
	if !ok {
		return
	}

	runs, err := s.migrations.ListRuns(r.Context(), userID, id)
	if err != nil {
		writeServiceError(w, err, "listing migration runs")
		return
	}

	response := make([]httpapi.MigrationRun, 0, len(runs))
	for index := range runs {
		response = append(response, runResponse(&runs[index]))
	}
	writeJSON(w, http.StatusOK, response)
}

func draftIDs(w http.ResponseWriter, r *http.Request, draftID httpapi.DraftId) (uint, uint, bool) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return 0, 0, false
	}
	id, ok := positiveID(w, draftID, "draft")
	return userID, id, ok
}

func draftResponse(d *migration.Draft) httpapi.MigrationDraft {
	return httpapi.MigrationDraft{
		Id:             int64(d.ID),
		DatabaseId:     int64(d.ManagedDatabaseID),
		Name:           d.Name,
		Sql:            d.SQL,
		Status:         httpapi.MigrationDraftStatus(d.Status),
		AppliedVersion: d.AppliedVersion,
		CreatedBy:      int64(d.CreatedBy),
		UpdatedBy:      int64(d.UpdatedBy),
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
	}
}

func runResponse(run *migration.Run) httpapi.MigrationRun {
	response := httpapi.MigrationRun{
		Id:         int64(run.ID),
		DraftId:    int64(run.DraftID),
		DatabaseId: int64(run.ManagedDatabaseID),
		Status:     httpapi.MigrationRunStatus(run.Status),
		Version:    run.Version,
		Sql:        run.SQL,
		ExecutedBy: int64(run.ExecutedBy),
		StartedAt:  run.StartedAt,
		FinishedAt: run.FinishedAt,
	}
	if run.ErrorCode != "" {
		response.ErrorCode = &run.ErrorCode
	}
	if run.ErrorMessage != "" {
		response.ErrorMessage = &run.ErrorMessage
	}
	return response
}
