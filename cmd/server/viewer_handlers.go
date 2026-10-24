package main

import (
	"net/http"
	httpapi "tubes-wbd/backend/http"
	"tubes-wbd/backend/viewer"
)

func (s apiServer) ListDatabaseSchemas(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	schemas, err := s.viewer.ListSchemas(r.Context(), userID, dbID)
	if err != nil {
		writeServiceError(w, err, "listing schemas")
		return
	}
	writeJSON(w, http.StatusOK, schemas)
}

func (s apiServer) ListDatabaseTables(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	params httpapi.ListDatabaseTablesParams,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	tables, err := s.viewer.ListTables(r.Context(), userID, dbID, params.Schema)
	if err != nil {
		writeServiceError(w, err, "listing tables")
		return
	}

	response := make([]httpapi.Table, 0, len(tables))
	for _, table := range tables {
		response = append(response, tableResponse(table))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) GetDatabaseTableStructure(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	params httpapi.GetDatabaseTableStructureParams,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	detail, err := s.viewer.DescribeTable(r.Context(), userID, dbID, params.Schema, params.Table)
	if err != nil {
		writeServiceError(w, err, "describing table")
		return
	}

	response := httpapi.TableStructure{
		Table:       tableResponse(detail.Table),
		Columns:     make([]httpapi.Column, 0, len(detail.Columns)),
		Constraints: make([]httpapi.Constraint, 0, len(detail.Constraints)),
		Indexes:     make([]httpapi.Index, 0, len(detail.Indexes)),
	}
	for _, c := range detail.Columns {
		response.Columns = append(response.Columns, httpapi.Column{
			Name:     c.Name,
			Type:     c.Type,
			Nullable: c.Nullable,
			Default:  c.Default,
		})
	}
	for _, c := range detail.Constraints {
		response.Constraints = append(response.Constraints, httpapi.Constraint{
			Name:       c.Name,
			Type:       c.Type,
			Definition: c.Definition,
		})
	}
	for _, i := range detail.Indexes {
		response.Indexes = append(response.Indexes, httpapi.Index{
			Name:       i.Name,
			Definition: i.Definition,
			Unique:     i.Unique,
			Primary:    i.Primary,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s apiServer) GetDatabaseTableRows(
	w http.ResponseWriter,
	r *http.Request,
	databaseID httpapi.DatabaseId,
	params httpapi.GetDatabaseTableRowsParams,
) {
	userID, dbID, ok := viewerIDs(w, r, databaseID)
	if !ok {
		return
	}

	page, err := s.viewer.TableData(r.Context(), userID, dbID,
		params.Schema, params.Table, intValue(params.Page), intValue(params.PageSize))
	if err != nil {
		writeServiceError(w, err, "reading table rows")
		return
	}

	writeJSON(w, http.StatusOK, httpapi.TableRows{
		Columns:  page.Columns,
		Rows:     page.Rows,
		Page:     page.Page,
		PageSize: page.PageSize,
		HasNext:  page.HasNext,
	})
}

func viewerIDs(w http.ResponseWriter, r *http.Request, databaseID httpapi.DatabaseId) (uint, uint, bool) {
	userID, ok := currentUserID(w, r)
	if !ok {
		return 0, 0, false
	}
	dbID, ok := positiveID(w, databaseID, "database")
	return userID, dbID, ok
}

func tableResponse(t viewer.Table) httpapi.Table {
	return httpapi.Table{Schema: t.Schema, Name: t.Name, Kind: string(t.Kind)}
}