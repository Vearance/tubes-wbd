// Frontend-only preview server with hard-coded users and in-memory sessions.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPreviewPort = "4173"
	previewCookieName  = "bscheme_preview_session"
	previewSessionTTL  = 30 * time.Minute
)

type previewAccount struct {
	Label             string
	Username          string
	Password          string
	CanEdit           bool
	IsAdmin           bool
	HasDatabaseAccess bool
}

type previewSession struct {
	Username          string
	CanEdit           bool
	IsAdmin           bool
	HasDatabaseAccess bool
	CSRFToken         string
	ExpiresAt         time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]previewSession
}

type loginPageData struct {
	Title           string
	Username        string
	CSRFToken       string
	Error           string
	SessionExpired  bool
	FieldErrors     map[string]string
	PreviewAccounts []previewAccount
}

type userView struct{ Username string }

type databaseView struct {
	ID          int
	Name        string
	Status      string
	Description string
	Permission  string
}

type schemaView struct {
	Name   string
	Tables []string
}

type schemaTableView struct {
	Name        string
	Columns     int
	Constraints int
	Indexes     int
}

type tableCellView struct {
	Value string
	Null  bool
}

type tableRowView struct {
	Cells []tableCellView
}

type adminUserView struct {
	Username    string
	Role        string
	Status      string
	StatusClass string
	Groups      int
	GroupNames  []adminUserGroupView
}

type adminUserGroupView struct {
	ID   int
	Name string
}

type adminGroupView struct {
	ID          int
	Name        string
	MemberCount int
	AccessCount int
}

type adminMemberView struct {
	Username string
	Role     string
}

type adminAccessView struct {
	Database   string
	Permission string
}

type adminGroupDetailView struct {
	Name    string
	Members []adminMemberView
	Access  []adminAccessView
}

type adminConnectionView struct {
	ID          int
	Name        string
	Host        string
	Port        int
	Database    string
	Username    string
	SSLMode     string
	HasPassword bool
	Status      string
	StatusClass string
}

type adminAuditView struct {
	Time         string
	Actor        string
	Action       string
	Outcome      string
	OutcomeClass string
}

type adminPageData struct {
	Title              string
	ActiveAdminSection string
	CurrentUser        userView
	CSRFToken          string
	Users              []adminUserView
	Groups             []adminGroupView
	Group              adminGroupDetailView
	Connections        []adminConnectionView
	AuditLogs          []adminAuditView
	Editing            bool
	FormUsername       string
	FormIsAdmin        bool
	Connection         adminConnectionView
	FormRole           string
	FormStatus         string
	FormError          string
	Notice             string
	GroupID            int
	AvailableUsers     []adminUserView
	AvailableDatabases []adminConnectionView
	ActorFilter        string
	OutcomeFilter      string
}

type migrationView struct {
	ID          int
	Version     string
	Name        string
	Executor    string
	ExecutedAt  string
	Status      string
	StatusClass string
	SQL         string
}

type pageData struct {
	Title           string
	Mode            string
	CanEdit         bool
	PermissionLabel string
	CurrentUser     userView
	CSRFToken       string
	ActiveDatabase  *databaseView
	Databases       []databaseView
	Schemas         []schemaView
	SchemaTables    []schemaTableView
	ActiveSchema    string
	ActiveTable     string
	ActiveTab       string
	ActiveSection   string
	Migrations      []migrationView
	Migration       migrationView
	QuerySQL        string
	QueryExecuted   bool
	QueryColumns    []string
	QueryRows       []tableRowView
	QueryTruncated  bool
	QueryPlan       string
	QueryError      string
}

var previewAccounts = map[string]previewAccount{
	"viewer": {Label: "View Only", Username: "viewer", Password: "preview-view", HasDatabaseAccess: true},
	"editor": {Label: "View + Edit", Username: "editor", Password: "preview-edit", CanEdit: true, HasDatabaseAccess: true},
	"admin":  {Label: "Platform Admin", Username: "admin", Password: "preview-admin", IsAdmin: true},
}

func main() {
	loginTemplate := mustParseTemplate(
		"frontend/templates/layouts/public.html",
		"frontend/templates/partials/form_feedback.html",
		"frontend/templates/pages/login.html",
	)
	databasesTemplate := mustParseAppPage("frontend/templates/pages/databases.html")
	workspaceTemplate := mustParseAppPage("frontend/templates/pages/database_workspace.html")
	historyTemplate := mustParseAppPage("frontend/templates/pages/migration_history.html")
	detailTemplate := mustParseAppPage("frontend/templates/pages/migration_detail.html")
	formTemplate := mustParseAppPage("frontend/templates/pages/migration_form.html")
	schemaTemplate := mustParseAppPage("frontend/templates/pages/schema_viewer.html")
	queryTemplate := mustParseAppPage("frontend/templates/pages/query_editor.html")
	adminUsersTemplate := mustParseAdminPage("frontend/templates/pages/admin_users.html")
	adminGroupsTemplate := mustParseAdminPage("frontend/templates/pages/admin_groups.html")
	adminGroupTemplate := mustParseAdminPage("frontend/templates/pages/admin_group_detail.html")
	adminDatabasesTemplate := mustParseAdminPage("frontend/templates/pages/admin_databases.html")
	adminAuditTemplate := mustParseAdminPage("frontend/templates/pages/admin_audit_logs.html")
	adminUserFormTemplate := mustParseAdminPage("frontend/templates/pages/admin_user_form.html")
	adminDatabaseFormTemplate := mustParseAdminPage("frontend/templates/pages/admin_database_form.html")

	store := &sessionStore{sessions: make(map[string]previewSession)}
	adminState := newAdminPreviewState()
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("frontend/static"))))

	requireSession := func(next func(http.ResponseWriter, *http.Request, previewSession)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			session, ok := store.get(r)
			if !ok {
				http.Redirect(w, r, "/login?state=expired", http.StatusSeeOther)
				return
			}
			next(w, r, session)
		}
	}
	requireDatabaseAccess := func(next func(http.ResponseWriter, *http.Request, previewSession)) http.HandlerFunc {
		return requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
			if !session.HasDatabaseAccess {
				http.Error(w, "database access required", http.StatusForbidden)
				return
			}
			next(w, r, session)
		})
	}
	requireAdmin := func(next func(http.ResponseWriter, *http.Request, previewSession)) http.HandlerFunc {
		return requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
			if !session.IsAdmin {
				http.Error(w, "Platform Admin access required", http.StatusForbidden)
				return
			}
			next(w, r, session)
		})
	}

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if session, ok := store.get(r); ok {
			if session.IsAdmin {
				http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			} else {
				http.Redirect(w, r, "/databases", http.StatusSeeOther)
			}
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if session, ok := store.get(r); ok {
			if session.IsAdmin {
				http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			} else {
				http.Redirect(w, r, "/databases", http.StatusSeeOther)
			}
			return
		}

		data := newPreviewLoginData()
		switch r.URL.Query().Get("state") {
		case "invalid":
			data.Username = "viewer"
			data.Error = "Invalid username or password."
		case "expired":
			data.SessionExpired = true
		case "server-error":
			data.Error = "Unable to sign in. Please try again later."
		case "fields":
			data.FieldErrors["username"] = "Enter your username."
			data.FieldErrors["password"] = "Enter your password."
		}
		render(w, loginTemplate, "public-layout", data)
	})

	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		data := newPreviewLoginData()
		data.Username = username

		if username == "" {
			data.FieldErrors["username"] = "Enter your username."
		}
		if password == "" {
			data.FieldErrors["password"] = "Enter your password."
		}
		if len(data.FieldErrors) > 0 {
			renderStatus(w, loginTemplate, "public-layout", data, http.StatusUnprocessableEntity)
			return
		}

		account, ok := previewAccounts[username]
		if !ok || subtle.ConstantTimeCompare([]byte(account.Password), []byte(password)) != 1 {
			data.Error = "Invalid username or password."
			renderStatus(w, loginTemplate, "public-layout", data, http.StatusUnauthorized)
			return
		}

		token, session := store.create(account)
		http.SetCookie(w, &http.Cookie{
			Name:     previewCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  session.ExpiresAt,
		})
		if account.IsAdmin {
			http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/databases", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /databases", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Your databases · BScheme"
		if session.HasDatabaseAccess {
			data.Databases = []databaseView{
				{ID: 1, Name: "Customer Database", Status: "Available", Permission: data.PermissionLabel},
				{ID: 2, Name: "Analytics Database", Status: "Available", Permission: data.PermissionLabel},
			}
		}
		data.ActiveDatabase = nil
		render(w, databasesTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Customer Database · BScheme"
		data.ActiveSchema = queryDefault(r, "schema", "public")
		data.ActiveTable = queryDefault(r, "table", "customers")
		data.ActiveTab = queryDefault(r, "tab", "data")
		data.ActiveSection = "table"
		render(w, workspaceTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/schemas/{schema}", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Schema viewer · BScheme"
		data.ActiveSchema = r.PathValue("schema")
		data.ActiveSection = "schema"
		data.SchemaTables = mockSchemaTables(data.ActiveSchema)
		render(w, schemaTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/query", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Query · BScheme"
		data.ActiveSection = "query"
		data.QuerySQL = "SELECT id, name, email, status\nFROM public.customers\nORDER BY id\nLIMIT 50"
		data.QueryExecuted = r.URL.Query().Get("executed") == "true"
		if data.QueryExecuted {
			data.QueryColumns = []string{"id", "name", "email", "status"}
			data.QueryRows = mockQueryRows()
		}
		render(w, queryTemplate, "app-layout", data)
	}))

	mux.HandleFunc("POST /databases/{databaseID}/query", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		data := mockPageData(session)
		data.Title = "Query · BScheme"
		data.ActiveSection = "query"
		data.QuerySQL = strings.TrimSpace(r.FormValue("sql"))
		if r.FormValue("action") == "explain" {
			data.QueryPlan = `{"Statements":[{"SQL":"` + data.QuerySQL + `","Plan":[{"Plan":{"Node Type":"Seq Scan"}}]}]}`
		} else {
			data.QueryExecuted = true
			data.QueryColumns = []string{"id", "name", "email", "status"}
			data.QueryRows = mockQueryRows()
		}
		render(w, queryTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/migrations", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Migration history · BScheme"
		data.ActiveSection = "migrations"
		data.Migrations = mockMigrations()
		render(w, historyTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/migrations/new", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "New migration · BScheme"
		data.ActiveSection = "migrations"
		if !session.CanEdit {
			renderStatus(w, formTemplate, "app-layout", data, http.StatusForbidden)
			return
		}
		render(w, formTemplate, "app-layout", data)
	}))

	mux.HandleFunc("POST /databases/{databaseID}/migrations", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !session.CanEdit {
			http.Error(w, "View + Edit access required", http.StatusForbidden)
			return
		}
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		http.Redirect(w, r, "/databases/1/migrations/4", http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/migrations/{migrationID}", requireDatabaseAccess(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Migration detail · BScheme"
		data.ActiveSection = "migrations"
		data.Migration = mockMigrations()[0]
		render(w, detailTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /admin", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /admin/users", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "users")
		data.Title = "Users · BScheme Admin"
		data.Users = adminState.usersList()
		if r.URL.Query().Has("saved") {
			data.Notice = "User saved in this preview."
		}
		render(w, adminUsersTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("GET /admin/users/new", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "users")
		data.Title = "New user · BScheme Admin"
		data.FormRole, data.FormStatus = "User", "Active"
		render(w, adminUserFormTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("GET /admin/users/{username}/edit", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "users")
		data.Title = "Edit user · BScheme Admin"
		data.Editing = true
		user, ok := adminState.user(r.PathValue("username"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		data.FormUsername, data.FormRole, data.FormStatus = user.Username, user.Role, user.Status
		render(w, adminUserFormTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("POST /admin/users/save", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		username := strings.TrimSpace(r.FormValue("username"))
		role, status := r.FormValue("role"), r.FormValue("status")
		editing := r.FormValue("editing") == "1"
		err := adminState.saveUser(username, r.FormValue("password"), role, status, editing)
		if err != "" {
			data := mockAdminPageData(session, "users")
			data.Title, data.FormUsername, data.FormRole, data.FormStatus = "User · BScheme Admin", username, role, status
			data.Editing, data.FormError = editing, err
			renderStatus(w, adminUserFormTemplate, "admin-layout", data, http.StatusUnprocessableEntity)
			return
		}
		http.Redirect(w, r, "/admin/users?saved=1", http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /admin/groups", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "groups")
		data.Title = "Groups · BScheme Admin"
		data.Groups = adminState.groupsList()
		if r.URL.Query().Has("saved") {
			data.Notice = "Group saved in this preview."
		}
		render(w, adminGroupsTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("GET /admin/groups/new", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "groups")
		data.Title = "New group · BScheme Admin"
		data.AvailableUsers, data.AvailableDatabases = adminState.usersList(), adminState.connectionsList()
		render(w, adminGroupTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("GET /admin/groups/{groupID}", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "groups")
		id, err := strconv.Atoi(r.PathValue("groupID"))
		group, ok := adminState.group(id)
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		data.Title = group.Name + " · BScheme Admin"
		data.Group, data.GroupID = group, id
		data.AvailableUsers, data.AvailableDatabases = adminState.usersList(), adminState.connectionsList()
		if r.URL.Query().Has("saved") {
			data.Notice = "Group changes saved in this preview."
		}
		render(w, adminGroupTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("POST /admin/groups/save", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("group_id"))
		if id < 0 {
			http.NotFound(w, r)
			return
		}
		if id > 0 {
			if _, ok := adminState.group(id); !ok {
				http.NotFound(w, r)
				return
			}
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			data := mockAdminPageData(session, "groups")
			data.Title, data.GroupID, data.FormError = "Group · BScheme Admin", id, "Enter a group name."
			data.Group = adminGroupDetailView{Name: name}
			data.AvailableUsers, data.AvailableDatabases = adminState.usersList(), adminState.connectionsList()
			renderStatus(w, adminGroupTemplate, "admin-layout", data, http.StatusUnprocessableEntity)
			return
		}
		groupID := adminState.saveGroup(id, name, r.Form["member"], r.Form["access_database"], r.Form["access_permission"])
		http.Redirect(w, r, fmt.Sprintf("/admin/groups/%d?saved=1", groupID), http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /admin/databases", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "databases")
		data.Title = "Database connections · BScheme Admin"
		data.Connections = adminState.connectionsList()
		if r.URL.Query().Has("saved") {
			data.Notice = "Connection saved in this preview."
		}
		render(w, adminDatabasesTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("GET /admin/databases/new", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "databases")
		data.Title = "Connect database · BScheme Admin"
		data.Connection = adminConnectionView{Port: 5432, SSLMode: "Require"}
		render(w, adminDatabaseFormTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("GET /admin/databases/{databaseID}/edit", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "databases")
		data.Title = "Edit connection · BScheme Admin"
		data.Editing = true
		id, err := strconv.Atoi(r.PathValue("databaseID"))
		connection, ok := adminState.connection(id)
		if err != nil || !ok {
			http.NotFound(w, r)
			return
		}
		data.Connection = connection
		render(w, adminDatabaseFormTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("POST /admin/databases/save", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		id, _ := strconv.Atoi(r.FormValue("connection_id"))
		port, _ := strconv.Atoi(r.FormValue("port"))
		connection := adminConnectionView{ID: id, Name: strings.TrimSpace(r.FormValue("display_name")), Host: strings.TrimSpace(r.FormValue("host")), Port: port, Database: strings.TrimSpace(r.FormValue("database")), Username: strings.TrimSpace(r.FormValue("username")), SSLMode: r.FormValue("ssl_mode")}
		err := adminState.saveConnection(connection, r.FormValue("password"))
		if err != "" {
			data := mockAdminPageData(session, "databases")
			data.Title, data.Editing, data.Connection, data.FormError = "Connection · BScheme Admin", id != 0, connection, err
			renderStatus(w, adminDatabaseFormTemplate, "admin-layout", data, http.StatusUnprocessableEntity)
			return
		}
		http.Redirect(w, r, "/admin/databases?saved=1", http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /admin/audit-logs", requireAdmin(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockAdminPageData(session, "audit")
		data.Title = "Audit logs · BScheme Admin"
		data.ActorFilter, data.OutcomeFilter = r.URL.Query().Get("actor"), r.URL.Query().Get("outcome")
		data.AuditLogs = adminState.auditList(data.ActorFilter, data.OutcomeFilter)
		render(w, adminAuditTemplate, "admin-layout", data)
	}))

	mux.HandleFunc("POST /logout", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		store.revoke(r)
		http.SetCookie(w, &http.Cookie{Name: previewCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}))

	port := os.Getenv("PREVIEW_PORT")
	if port == "" {
		port = defaultPreviewPort
	}
	server := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("frontend preview available at http://localhost:%s/login", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(fmt.Errorf("serve frontend preview: %w", err))
	}
}

func (s *sessionStore) create(account previewAccount) (string, previewSession) {
	token := randomHex(32)
	session := previewSession{
		Username:          account.Username,
		CanEdit:           account.CanEdit,
		IsAdmin:           account.IsAdmin,
		HasDatabaseAccess: account.HasDatabaseAccess,
		CSRFToken:         randomHex(24),
		ExpiresAt:         time.Now().Add(previewSessionTTL),
	}
	s.mu.Lock()
	s.sessions[token] = session
	s.mu.Unlock()
	return token, session
}

func (s *sessionStore) get(r *http.Request) (previewSession, bool) {
	cookie, err := r.Cookie(previewCookieName)
	if err != nil {
		return previewSession{}, false
	}
	s.mu.RLock()
	session, ok := s.sessions[cookie.Value]
	s.mu.RUnlock()
	if !ok || time.Now().After(session.ExpiresAt) {
		if ok {
			s.mu.Lock()
			delete(s.sessions, cookie.Value)
			s.mu.Unlock()
		}
		return previewSession{}, false
	}
	return session, true
}

func (s *sessionStore) revoke(r *http.Request) {
	cookie, err := r.Cookie(previewCookieName)
	if err != nil {
		return
	}
	s.mu.Lock()
	delete(s.sessions, cookie.Value)
	s.mu.Unlock()
}

func randomHex(size int) string {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(fmt.Errorf("generate preview token: %w", err))
	}
	return hex.EncodeToString(value)
}

func validCSRF(r *http.Request, session previewSession) bool {
	if err := r.ParseForm(); err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(r.FormValue("csrf_token")), []byte(session.CSRFToken)) == 1
}

func newPreviewLoginData() loginPageData {
	return loginPageData{
		Title:       "Sign in · BScheme",
		FieldErrors: make(map[string]string),
		PreviewAccounts: []previewAccount{
			previewAccounts["viewer"],
			previewAccounts["editor"],
			previewAccounts["admin"],
		},
	}
}

func mustParseTemplate(files ...string) *template.Template {
	sharedPartials := []string{
		"frontend/templates/partials/icons.html",
		"frontend/templates/partials/sidebar_account.html",
		"frontend/templates/partials/admin_sidebar_navigation.html",
		"frontend/templates/partials/database_sidebar_entities.html",
		"frontend/templates/partials/database_sidebar_workspace.html",
	}
	files = append(sharedPartials, files...)
	tmpl, err := template.ParseFiles(files...)
	if err != nil {
		log.Fatalf("parse template: %v", err)
	}
	return tmpl
}

func mustParseAppPage(page string) *template.Template {
	return mustParseTemplate(
		"frontend/templates/layouts/app.html",
		"frontend/templates/partials/app_header.html",
		"frontend/templates/partials/database_sidebar.html",
		"frontend/templates/partials/database_list_sidebar.html",
		page,
	)
}

func mustParseAdminPage(page string) *template.Template {
	return mustParseTemplate(
		"frontend/templates/layouts/admin.html",
		"frontend/templates/partials/admin_header.html",
		"frontend/templates/partials/admin_sidebar.html",
		page,
	)
}

func render(w http.ResponseWriter, tmpl *template.Template, name string, data any) {
	renderStatus(w, tmpl, name, data, http.StatusOK)
}

func renderStatus(w http.ResponseWriter, tmpl *template.Template, name string, data any, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("render template %q: %v", name, err)
	}
}

func mockAdminPageData(session previewSession, section string) adminPageData {
	return adminPageData{
		ActiveAdminSection: section,
		CurrentUser:        userView{Username: session.Username},
		CSRFToken:          session.CSRFToken,
	}
}

func mockAdminUsers() []adminUserView {
	return []adminUserView{
		{Username: "admin", Role: "Platform Admin", Status: "Active", StatusClass: "status-success", Groups: 0},
		{Username: "editor", Role: "User", Status: "Active", StatusClass: "status-success", Groups: 1},
		{Username: "viewer", Role: "User", Status: "Active", StatusClass: "status-success", Groups: 1},
		{Username: "bob", Role: "User", Status: "Disabled", StatusClass: "status-inactive", Groups: 1},
	}
}

func mockAdminGroups() []adminGroupView {
	return []adminGroupView{
		{ID: 1, Name: "Developers", MemberCount: 2, AccessCount: 2},
		{ID: 2, Name: "Analysts", MemberCount: 3, AccessCount: 1},
	}
}

func mockAdminGroupDetail() adminGroupDetailView {
	return adminGroupDetailView{
		Name: "Developers",
		Members: []adminMemberView{
			{Username: "editor", Role: "User"},
			{Username: "bob", Role: "User"},
		},
		Access: []adminAccessView{
			{Database: "Customer Database", Permission: "View + Edit"},
			{Database: "Analytics Database", Permission: "View Only"},
		},
	}
}

func mockAdminConnections() []adminConnectionView {
	return []adminConnectionView{
		{Name: "Customer Database", Host: "db.internal", Port: 5432, Database: "customer", Status: "Connected", StatusClass: "status-success"},
		{Name: "Analytics Database", Host: "analytics.internal", Port: 5432, Database: "analytics", Status: "Unavailable", StatusClass: "status-danger"},
	}
}

func mockAdminAuditLogs() []adminAuditView {
	return []adminAuditView{
		{Time: "Oct 24 18:42", Actor: "admin", Action: "group.access.updated", Outcome: "Success", OutcomeClass: "status-success"},
		{Time: "Oct 24 18:10", Actor: "editor", Action: "migration.executed", Outcome: "Success", OutcomeClass: "status-success"},
		{Time: "Oct 24 17:58", Actor: "viewer", Action: "login.failed", Outcome: "Failed", OutcomeClass: "status-danger"},
	}
}

func mockPageData(session previewSession) pageData {
	mode := "view"
	permission := "View Only"
	if session.CanEdit {
		mode = "edit"
		permission = "View + Edit"
	}
	database := databaseView{ID: 1, Name: "Customer Database", Status: "Connected"}
	return pageData{
		Mode:            mode,
		CanEdit:         session.CanEdit,
		PermissionLabel: permission,
		CurrentUser:     userView{Username: session.Username},
		CSRFToken:       session.CSRFToken,
		ActiveDatabase:  &database,
		Schemas: []schemaView{
			{Name: "public", Tables: []string{"customers", "orders", "products"}},
			{Name: "reporting", Tables: []string{"monthly_sales"}},
		},
		ActiveSchema: "public",
		ActiveTable:  "customers",
		ActiveTab:    "data",
	}
}

func mockQueryRows() []tableRowView {
	return []tableRowView{
		{Cells: []tableCellView{{Value: "1"}, {Value: "Alice"}, {Value: "alice@example.com"}, {Value: "active"}}},
		{Cells: []tableCellView{{Value: "2"}, {Value: "Bob"}, {Null: true}, {Value: "inactive"}}},
	}
}

func mockSchemaTables(schema string) []schemaTableView {
	if schema == "reporting" {
		return []schemaTableView{{Name: "monthly_sales", Columns: 6, Constraints: 1, Indexes: 2}}
	}
	return []schemaTableView{
		{Name: "customers", Columns: 4, Constraints: 2, Indexes: 2},
		{Name: "orders", Columns: 7, Constraints: 3, Indexes: 3},
		{Name: "products", Columns: 5, Constraints: 2, Indexes: 2},
	}
}

func mockMigrations() []migrationView {
	return []migrationView{
		{ID: 4, Version: "004", Name: "Add customer phone number", Executor: "editor", ExecutedAt: "Draft", Status: "Draft", StatusClass: "status-inactive", SQL: "ALTER TABLE public.customers\nADD COLUMN phone_number TEXT;"},
		{ID: 3, Version: "003", Name: "Add order status", Executor: "editor", ExecutedAt: "Oct 23 14:15", Status: "Success", StatusClass: "status-success"},
		{ID: 2, Version: "002", Name: "Create products", Executor: "editor", ExecutedAt: "Oct 22 09:20", Status: "Failed", StatusClass: "status-danger"},
	}
}

func queryDefault(r *http.Request, name, fallback string) string {
	if value := r.URL.Query().Get(name); value != "" {
		return value
	}
	return fallback
}

// adminPreviewState holds temporary UI data. It does not change the real API or PostgreSQL.
type adminPreviewState struct {
	mu                        sync.RWMutex
	users                     map[string]adminUserView
	groups                    map[int]adminGroupDetailView
	connections               map[int]adminConnectionView
	audit                     []adminAuditView
	nextGroup, nextConnection int
}

func newAdminPreviewState() *adminPreviewState {
	s := &adminPreviewState{users: map[string]adminUserView{}, groups: map[int]adminGroupDetailView{}, connections: map[int]adminConnectionView{}, audit: mockAdminAuditLogs(), nextGroup: 3, nextConnection: 3}
	for _, user := range mockAdminUsers() {
		s.users[user.Username] = user
	}
	s.groups[1] = mockAdminGroupDetail()
	s.groups[2] = adminGroupDetailView{Name: "Analysts", Members: []adminMemberView{{Username: "viewer", Role: "User"}, {Username: "editor", Role: "User"}}, Access: []adminAccessView{{Database: "Analytics Database", Permission: "View Only"}}}
	for i, connection := range mockAdminConnections() {
		connection.ID = i + 1
		connection.Username = "db_manager"
		connection.SSLMode = "Require"
		connection.HasPassword = true
		s.connections[connection.ID] = connection
	}
	return s
}

func (s *adminPreviewState) usersList() []adminUserView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]adminUserView, 0, len(s.users))
	for _, user := range s.users {
		user.Groups = 0
		user.GroupNames = nil
		for id, group := range s.groups {
			for _, member := range group.Members {
				if member.Username == user.Username {
					user.Groups++
					user.GroupNames = append(user.GroupNames, adminUserGroupView{ID: id, Name: group.Name})
				}
			}
		}
		sort.Slice(user.GroupNames, func(i, j int) bool { return user.GroupNames[i].Name < user.GroupNames[j].Name })
		result = append(result, user)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Username < result[j].Username })
	return result
}

func (s *adminPreviewState) user(username string) (adminUserView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[username]
	return user, ok
}

func (s *adminPreviewState) saveUser(username, password, role, status string, editing bool) string {
	if username == "" || strings.ContainsAny(username, " /\\\t\n") {
		return "Enter a username without spaces or slashes."
	}
	if role != "User" && role != "Platform Admin" {
		return "Choose a valid platform role."
	}
	if status != "Active" && status != "Disabled" {
		return "Choose a valid status."
	}
	if !editing && password == "" {
		return "Enter a password for the new user."
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, exists := s.users[username]
	if editing && !exists {
		return "User no longer exists."
	}
	if !editing && exists {
		return "That username is already in use."
	}
	statusClass := "status-success"
	if status == "Disabled" {
		statusClass = "status-inactive"
	}
	s.users[username] = adminUserView{Username: username, Role: role, Status: status, StatusClass: statusClass}
	s.record("admin", "user.saved", "Success")
	return ""
}

func (s *adminPreviewState) groupsList() []adminGroupView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]adminGroupView, 0, len(s.groups))
	for id, group := range s.groups {
		result = append(result, adminGroupView{ID: id, Name: group.Name, MemberCount: len(group.Members), AccessCount: len(group.Access)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *adminPreviewState) group(id int) (adminGroupDetailView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	group, ok := s.groups[id]
	if !ok {
		return group, false
	}
	group.Members = append([]adminMemberView(nil), group.Members...)
	group.Access = append([]adminAccessView(nil), group.Access...)
	return group, true
}

func (s *adminPreviewState) saveGroup(id int, name string, members, databases, permissions []string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == 0 {
		id = s.nextGroup
		s.nextGroup++
	}
	group := adminGroupDetailView{Name: name}
	seenMembers := map[string]bool{}
	for _, username := range members {
		user, ok := s.users[username]
		if ok && !seenMembers[username] {
			group.Members = append(group.Members, adminMemberView{Username: username, Role: user.Role})
			seenMembers[username] = true
		}
	}
	seenDatabases := map[string]bool{}
	for i, database := range databases {
		if i >= len(permissions) || seenDatabases[database] {
			continue
		}
		permission := permissions[i]
		if permission != "View Only" && permission != "View + Edit" {
			continue
		}
		for _, connection := range s.connections {
			if connection.Name == database {
				group.Access = append(group.Access, adminAccessView{Database: database, Permission: permission})
				seenDatabases[database] = true
				break
			}
		}
	}
	s.groups[id] = group
	s.record("admin", "group.saved", "Success")
	return id
}

func (s *adminPreviewState) connectionsList() []adminConnectionView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]adminConnectionView, 0, len(s.connections))
	for _, connection := range s.connections {
		result = append(result, connection)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *adminPreviewState) connection(id int) (adminConnectionView, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.connections[id]
	return item, ok
}

func (s *adminPreviewState) saveConnection(connection adminConnectionView, password string) string {
	if connection.Name == "" || connection.Host == "" || connection.Database == "" || connection.Username == "" {
		return "Complete all required connection fields."
	}
	if connection.Port < 1 || connection.Port > 65535 {
		return "Enter a port between 1 and 65535."
	}
	if connection.SSLMode != "Require" && connection.SSLMode != "Prefer" && connection.SSLMode != "Disable" {
		return "Choose a valid SSL mode."
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.connections {
		if id != connection.ID && strings.EqualFold(existing.Name, connection.Name) {
			return "A connection with this display name already exists."
		}
	}
	if connection.ID == 0 {
		connection.ID = s.nextConnection
		s.nextConnection++
	} else if old, ok := s.connections[connection.ID]; ok {
		connection.HasPassword = old.HasPassword
		if old.Name != connection.Name {
			for id, group := range s.groups {
				for i := range group.Access {
					if group.Access[i].Database == old.Name {
						group.Access[i].Database = connection.Name
					}
				}
				s.groups[id] = group
			}
		}
	} else {
		return "Connection no longer exists."
	}
	if password != "" {
		connection.HasPassword = true
	}
	if !connection.HasPassword {
		return "Enter a password for the new connection."
	}
	connection.Status, connection.StatusClass = "Not tested", "status-inactive"
	s.connections[connection.ID] = connection
	s.record("admin", "database.connection.saved", "Success")
	return ""
}

func (s *adminPreviewState) auditList(actor, outcome string) []adminAuditView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]adminAuditView, 0, len(s.audit))
	for _, entry := range s.audit {
		if (actor == "" || actor == entry.Actor) && (outcome == "" || outcome == entry.Outcome) {
			result = append(result, entry)
		}
	}
	return result
}

func (s *adminPreviewState) record(actor, action, outcome string) {
	entry := adminAuditView{Time: time.Now().Format("Jan 02 15:04"), Actor: actor, Action: action, Outcome: outcome, OutcomeClass: "status-success"}
	s.audit = append([]adminAuditView{entry}, s.audit...)
}
