package frontend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/manageddb"
	"tubes-wbd/backend/migration"
)

func (h *Handler) listDatabases(w http.ResponseWriter, r *http.Request, session pageSession) {
	databases, err := h.deps.ManagedDatabase.List(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to load databases")
		return
	}
	data := h.basePage(session)
	data.Title = "Your databases · BScheme"
	for index := range databases {
		permission, err := h.deps.Authorization.EffectivePermission(r.Context(), session.User.ID, databases[index].ID)
		if err != nil {
			h.renderError(w, http.StatusInternalServerError, "Unable to load database permissions")
			return
		}
		if permission == access.PermissionNone {
			continue
		}
		view := h.databaseView(&databases[index], permission)
		data.Databases = append(data.Databases, view)
	}
	h.render(w, "databases", "app-layout", data, http.StatusOK)
}

func (h *Handler) workspace(w http.ResponseWriter, r *http.Request, session pageSession) {
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	data.Title = db.Name + " · BScheme"
	data.ActiveSection = "table"
	data.ActiveTab = h.queryValue(r, "tab", "data")
	h.render(w, "workspace", "app-layout", data, http.StatusOK)
}

func (h *Handler) schema(w http.ResponseWriter, r *http.Request, session pageSession) {
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	schemaName := r.PathValue("schema")
	tables, err := h.deps.Viewer.ListTables(r.Context(), session.User.ID, db.ID, schemaName)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	data.Title = schemaName + " · " + db.Name
	data.ActiveSection = "schema"
	data.ActiveSchema = schemaName
	for _, table := range tables {
		detail, err := h.deps.Viewer.DescribeTable(r.Context(), session.User.ID, db.ID, table.Schema, table.Name)
		if err != nil {
			h.renderDatabaseError(w, err)
			return
		}
		data.SchemaTables = append(data.SchemaTables, schemaTableView{
			Name:        detail.Name,
			Columns:     len(detail.Columns),
			Constraints: len(detail.Constraints),
			Indexes:     len(detail.Indexes),
		})
	}
	h.render(w, "schema", "app-layout", data, http.StatusOK)
}

func (h *Handler) queryGet(w http.ResponseWriter, r *http.Request, session pageSession) {
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	data.Title = "Query · " + db.Name
	data.ActiveSection = "query"
	data.QuerySQL = "SELECT * FROM public.customers LIMIT 50;"
	h.render(w, "query", "app-layout", data, http.StatusOK)
}

func (h *Handler) queryPost(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form")
		return
	}
	sql := strings.TrimSpace(r.FormValue("sql"))
	result, err := h.deps.Migrations.Explain(r.Context(), session.User.ID, db.ID, sql)
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	data.Title = "Query · " + db.Name
	data.ActiveSection = "query"
	data.QuerySQL = sql
	if err != nil {
		data.QueryExecuted = false
		data.QueryPlan = err.Error()
		h.render(w, "query", "app-layout", data, http.StatusUnprocessableEntity)
		return
	}
	plan, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to format query plan")
		return
	}
	data.QueryExecuted = true
	data.QueryPlan = string(plan)
	h.render(w, "query", "app-layout", data, http.StatusOK)
}

func (h *Handler) migrationHistory(w http.ResponseWriter, r *http.Request, session pageSession) {
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	history, err := h.deps.Migrations.History(r.Context(), session.User.ID, db.ID, 1, 50)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data.Title = "Migration history · " + db.Name
	data.ActiveSection = "migrations"
	for _, entry := range history.Entries {
		data.Migrations = append(data.Migrations, h.migrationSummary(r, entry))
	}
	if r.URL.Query().Has("saved") {
		data.Notice = "Migration draft saved."
	}
	h.render(w, "history", "app-layout", data, http.StatusOK)
}

func (h *Handler) migrationForm(w http.ResponseWriter, r *http.Request, session pageSession) {
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	data.Title = "New migration · " + db.Name
	data.ActiveSection = "migrations"
	if permission != access.PermissionViewEdit {
		data.FormError = "You need View + Edit access to create migrations."
	}
	h.render(w, "migration-form", "app-layout", data, http.StatusOK)
}

func (h *Handler) createMigration(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	if permission != access.PermissionViewEdit {
		h.renderError(w, http.StatusForbidden, "View + Edit access required")
		return
	}
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form")
		return
	}
	draft, err := h.deps.Migrations.CreateDraft(r.Context(), session.User.ID, db.ID, migration.Migration{
		Name: r.FormValue("name"),
		SQL:  r.FormValue("sql"),
	})
	if err != nil {
		data, ok := h.workspaceData(w, r, session, db, permission)
		if !ok {
			return
		}
		data.Title = "New migration · " + db.Name
		data.ActiveSection = "migrations"
		data.FormError = err.Error()
		h.render(w, "migration-form", "app-layout", data, http.StatusUnprocessableEntity)
		return
	}
	_ = draft
	h.redirect(w, r, fmt.Sprintf("/databases/%d/migrations?saved=1", db.ID))
}

func (h *Handler) migrationDetail(w http.ResponseWriter, r *http.Request, session pageSession) {
	db, permission, err := h.databaseContext(r, session)
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	version, err := h.parseID(r, "version")
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid migration version")
		return
	}
	entry, err := h.deps.Migrations.HistoryEntry(r.Context(), session.User.ID, db.ID, int64(version))
	if err != nil {
		h.renderDatabaseError(w, err)
		return
	}
	data, ok := h.workspaceData(w, r, session, db, permission)
	if !ok {
		return
	}
	data.Title = entry.Name + " · " + db.Name
	data.ActiveSection = "migrations"
	data.Migration = migrationView{
		ID:          entry.Version,
		Version:     entry.Version,
		Name:        entry.Name,
		Executor:    h.userName(r, entry.ExecutedBy),
		ExecutedAt:  entry.ExecutedAt.Format("2006-01-02 15:04:05 MST"),
		Status:      "Applied",
		StatusClass: "status-success",
		SQL:         entry.SQL,
	}
	h.render(w, "migration-detail", "app-layout", data, http.StatusOK)
}

func (h *Handler) workspaceData(w http.ResponseWriter, r *http.Request, session pageSession, db *manageddb.ManagedDatabase, permission access.PermissionLevel) (pageData, bool) {
	data := h.basePage(session)
	activeDB := h.databaseView(db, permission)
	data.ActiveDatabase = &activeDB
	data.PermissionLabel = activeDB.Permission
	data.CanEdit = activeDB.CanEdit
	data.ActiveSchema = h.queryValue(r, "schema", "public")
	data.ActiveTable = h.queryValue(r, "table", "")

	schemas, err := h.deps.Viewer.ListSchemas(r.Context(), session.User.ID, db.ID)
	if err != nil {
		h.renderDatabaseError(w, err)
		return data, false
	}
	for _, schemaName := range schemas {
		tables, err := h.deps.Viewer.ListTables(r.Context(), session.User.ID, db.ID, schemaName)
		if err != nil {
			h.renderDatabaseError(w, err)
			return data, false
		}
		schema := schemaView{Name: schemaName}
		for _, table := range tables {
			schema.Tables = append(schema.Tables, table.Name)
		}
		data.Schemas = append(data.Schemas, schema)
		if schemaName == data.ActiveSchema && data.ActiveTable == "" && len(schema.Tables) > 0 {
			data.ActiveTable = schema.Tables[0]
		}
	}
	if data.ActiveSchema == "" && len(data.Schemas) > 0 {
		data.ActiveSchema = data.Schemas[0].Name
		if data.ActiveTable == "" && len(data.Schemas[0].Tables) > 0 {
			data.ActiveTable = data.Schemas[0].Tables[0]
		}
	}
	if data.ActiveTable != "" {
		detail, err := h.deps.Viewer.DescribeTable(r.Context(), session.User.ID, db.ID, data.ActiveSchema, data.ActiveTable)
		if err != nil {
			h.renderDatabaseError(w, err)
			return data, false
		}
		data.TableDetail = detail
		rows, err := h.deps.Viewer.TableData(r.Context(), session.User.ID, db.ID, data.ActiveSchema, data.ActiveTable, 1, 0)
		if err != nil {
			h.renderDatabaseError(w, err)
			return data, false
		}
		data.TablePage = rows.Page
		data.TablePageSize = rows.PageSize
		data.TableHasNext = rows.HasNext
		for _, row := range rows.Rows {
			view := tableRowView{Cells: make([]tableCellView, 0, len(row))}
			for _, value := range row {
				if value == nil {
					view.Cells = append(view.Cells, tableCellView{Null: true})
					continue
				}
				view.Cells = append(view.Cells, tableCellView{Value: *value})
			}
			data.TableRows = append(data.TableRows, view)
		}
	}
	return data, true
}

func (h *Handler) migrationSummary(r *http.Request, entry migration.HistorySummary) migrationView {
	return migrationView{
		ID:          entry.Version,
		Version:     entry.Version,
		Name:        entry.Name,
		Executor:    h.userName(r, entry.ExecutedBy),
		ExecutedAt:  entry.ExecutedAt.Format("2006-01-02 15:04:05 MST"),
		Status:      "Applied",
		StatusClass: "status-success",
	}
}

func (h *Handler) userName(r *http.Request, id uint) string {
	user, err := h.deps.Users.FindByID(r.Context(), id)
	if err != nil || user == nil {
		return fmt.Sprintf("User %d", id)
	}
	return user.Username
}

func (h *Handler) renderDatabaseError(w http.ResponseWriter, err error) {
	if w == nil {
		return
	}
	status := http.StatusInternalServerError
	if errors.Is(err, access.ErrInsufficientPermission) {
		status = http.StatusForbidden
	}
	h.renderError(w, status, err.Error())
}
