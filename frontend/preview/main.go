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
	Label    string
	Username string
	Password string
	CanEdit  bool
}

type previewSession struct {
	Username  string
	CanEdit   bool
	CSRFToken string
	ExpiresAt time.Time
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
}

var previewAccounts = map[string]previewAccount{
	"viewer": {Label: "View Only", Username: "viewer", Password: "preview-view", CanEdit: false},
	"editor": {Label: "View + Edit", Username: "editor", Password: "preview-edit", CanEdit: true},
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

	store := &sessionStore{sessions: make(map[string]previewSession)}
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

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := store.get(r); ok {
			http.Redirect(w, r, "/databases", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := store.get(r); ok {
			http.Redirect(w, r, "/databases", http.StatusSeeOther)
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
		http.Redirect(w, r, "/databases", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /databases", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Your databases · BScheme"
		data.Databases = []databaseView{
			{ID: 1, Name: "Customer Database", Status: "Available"},
			{ID: 2, Name: "Analytics Database", Status: "Available"},
		}
		data.ActiveDatabase = nil
		render(w, databasesTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Customer Database · BScheme"
		data.ActiveSchema = queryDefault(r, "schema", "public")
		data.ActiveTable = queryDefault(r, "table", "customers")
		data.ActiveTab = queryDefault(r, "tab", "data")
		data.ActiveSection = "table"
		render(w, workspaceTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/schemas/{schema}", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Schema viewer · BScheme"
		data.ActiveSchema = r.PathValue("schema")
		data.ActiveSection = "schema"
		data.SchemaTables = mockSchemaTables(data.ActiveSchema)
		render(w, schemaTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/query", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Query · BScheme"
		data.ActiveSection = "query"
		data.QuerySQL = "SELECT id, name, email, status\nFROM public.customers\nORDER BY id\nLIMIT 50;"
		data.QueryExecuted = r.URL.Query().Get("executed") == "true"
		render(w, queryTemplate, "app-layout", data)
	}))

	mux.HandleFunc("POST /databases/{databaseID}/query", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		if !validCSRF(r, session) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		http.Redirect(w, r, "/databases/1/query?executed=true", http.StatusSeeOther)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/migrations", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Migration history · BScheme"
		data.ActiveSection = "migrations"
		data.Migrations = mockMigrations()
		render(w, historyTemplate, "app-layout", data)
	}))

	mux.HandleFunc("GET /databases/{databaseID}/migrations/new", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "New migration · BScheme"
		data.ActiveSection = "migrations"
		if !session.CanEdit {
			renderStatus(w, formTemplate, "app-layout", data, http.StatusForbidden)
			return
		}
		render(w, formTemplate, "app-layout", data)
	}))

	mux.HandleFunc("POST /databases/{databaseID}/migrations", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
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

	mux.HandleFunc("GET /databases/{databaseID}/migrations/{migrationID}", requireSession(func(w http.ResponseWriter, r *http.Request, session previewSession) {
		data := mockPageData(session)
		data.Title = "Migration detail · BScheme"
		data.ActiveSection = "migrations"
		data.Migration = mockMigrations()[0]
		render(w, detailTemplate, "app-layout", data)
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
		Username:  account.Username,
		CanEdit:   account.CanEdit,
		CSRFToken: randomHex(24),
		ExpiresAt: time.Now().Add(previewSessionTTL),
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
		},
	}
}

func mustParseTemplate(files ...string) *template.Template {
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
