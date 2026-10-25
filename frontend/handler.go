package frontend

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/http/middleware"
	"tubes-wbd/backend/manageddb"
	"tubes-wbd/backend/migration"
	"tubes-wbd/backend/viewer"
)

//go:embed templates static
var frontendFiles embed.FS

const (
	BasePath       = "/app"
	CSRFCookieName = "labpro_sucks_csrf"
)

type Dependencies struct {
	Auth            *auth.AuthService
	Users           auth.UserManagementRepository
	UserManagement  *auth.UserManagementService
	Groups          *access.GroupService
	Authorization   *access.AuthorizationService
	ManagedDatabase *manageddb.ManagedDBService
	Viewer          *viewer.ViewerService
	Migrations      *migration.MigrationService
	CookieOptions   middleware.CookieOptions
}

type Handler struct {
	deps      Dependencies
	router    chi.Router
	templates map[string]*template.Template
}

type pageSession struct {
	Token     string
	Auth      auth.AuthenticatedSession
	User      *auth.User
	CSRFToken string
}

type currentUserView struct {
	ID       uint
	Username string
	IsAdmin  bool
}

type databaseView struct {
	ID          uint
	Name        string
	Host        string
	Port        uint16
	Database    string
	Username    string
	SSLMode     string
	Status      string
	StatusClass string
	CanEdit     bool
	Permission  string
	HasPassword bool
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

type tableRowView struct {
	Cells []tableCellView
}

type tableCellView struct {
	Value string
	Null  bool
}

type loginPageData struct {
	Title          string
	Username       string
	CSRFToken      string
	Error          string
	SessionExpired bool
	FieldErrors    map[string]string
}

type pageData struct {
	Title           string
	Mode            string
	CanEdit         bool
	PermissionLabel string
	CurrentUser     currentUserView
	CSRFToken       string
	ActiveDatabase  *databaseView
	Databases       []databaseView
	Schemas         []schemaView
	SchemaTables    []schemaTableView
	TableDetail     *viewer.TableDetail
	TableRows       []tableRowView
	TablePage       int
	TablePageSize   int
	TableHasNext    bool
	ActiveSchema    string
	ActiveTable     string
	ActiveTab       string
	ActiveSection   string
	Migrations      []migrationView
	Drafts          []migrationDraftSummaryView
	Migration       migrationView
	MigrationDraft  *migrationDraftView
	MigrationRuns   []migrationRunView
	QuerySQL        string
	QueryExecuted   bool
	QueryColumns    []string
	QueryRows       []tableRowView
	QueryTruncated  bool
	QueryPlan       string
	QueryError      string
	Notice          string
	FormError       string
}

type migrationView struct {
	ID          int64
	Version     int64
	Name        string
	Executor    string
	ExecutedAt  string
	Status      string
	StatusClass string
	SQL         string
}

type migrationDraftSummaryView struct {
	ID          uint
	Name        string
	Status      string
	StatusClass string
	UpdatedAt   string
}

type migrationDraftView struct {
	ID             uint
	Name           string
	SQL            string
	Status         string
	StatusClass    string
	AppliedVersion string
	CreatedBy      string
	UpdatedBy      string
	CreatedAt      string
	UpdatedAt      string
	CanEdit        bool
	CanExecute     bool
}

type migrationRunView struct {
	Status       string
	StatusClass  string
	Version      string
	ExecutedBy   string
	StartedAt    string
	FinishedAt   string
	ErrorMessage string
}

type adminUserView struct {
	ID          uint
	Username    string
	Role        string
	Status      string
	StatusClass string
	Groups      []adminUserGroupView
	GroupNames  []adminUserGroupView
}

type adminUserGroupView struct {
	ID   uint
	Name string
}

type adminGroupView struct {
	ID          uint
	Name        string
	MemberCount int
	AccessCount int
}

type adminMemberView struct {
	ID       uint
	Username string
	Role     string
}

type adminAccessView struct {
	ID         uint
	Database   string
	DatabaseID uint
	Permission string
}

type adminGroupDetailView struct {
	Name    string
	Members []adminMemberView
	Access  []adminAccessView
}

type adminPageData struct {
	Title              string
	ActiveAdminSection string
	CurrentUser        currentUserView
	CSRFToken          string
	Users              []adminUserView
	Groups             []adminGroupView
	Group              adminGroupDetailView
	Connections        []databaseView
	AuditLogs          []adminAuditView
	Editing            bool
	FormUsername       string
	FormRole           string
	FormStatus         string
	FormError          string
	Notice             string
	GroupID            uint
	AvailableUsers     []adminUserView
	AvailableDatabases []databaseView
	ActorFilter        string
	OutcomeFilter      string
	Connection         databaseView
}

type adminAuditView struct {
	Time         string
	Actor        string
	Action       string
	Outcome      string
	OutcomeClass string
}

func NewHandler(deps Dependencies) (*Handler, error) {
	if deps.Auth == nil {
		return nil, errors.New("frontend auth service is nil")
	}
	if deps.Users == nil {
		return nil, errors.New("frontend user repository is nil")
	}
	if deps.UserManagement == nil {
		return nil, errors.New("frontend user management service is nil")
	}
	if deps.Groups == nil {
		return nil, errors.New("frontend group service is nil")
	}
	if deps.Authorization == nil {
		return nil, errors.New("frontend authorization service is nil")
	}
	if deps.ManagedDatabase == nil {
		return nil, errors.New("frontend managed database service is nil")
	}
	if deps.Viewer == nil {
		return nil, errors.New("frontend viewer service is nil")
	}
	if deps.Migrations == nil {
		return nil, errors.New("frontend migration service is nil")
	}

	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}

	h := &Handler{deps: deps, templates: templates}
	h.router = chi.NewRouter()
	h.routes()
	return h, nil
}

func parseTemplates() (map[string]*template.Template, error) {
	pages := map[string]struct {
		layout string
		page   string
		files  []string
	}{
		"login": {
			layout: "templates/layouts/public.html",
			page:   "templates/pages/login.html",
			files:  []string{"templates/partials/form_feedback.html"},
		},
		"databases": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/databases.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_list_sidebar.html"},
		},
		"workspace": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/database_workspace.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"schema": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/schema_viewer.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"query": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/query_editor.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"history": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/migration_history.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"migration-form": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/migration_form.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"migration-detail": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/migration_detail.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"migration-draft": {
			layout: "templates/layouts/app.html",
			page:   "templates/pages/migration_draft.html",
			files:  []string{"templates/partials/app_header.html", "templates/partials/database_sidebar.html"},
		},
		"admin-users": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_users.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
		"admin-user-form": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_user_form.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
		"admin-groups": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_groups.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
		"admin-group": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_group_detail.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
		"admin-databases": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_databases.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
		"admin-database-form": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_database_form.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
		"admin-audit": {
			layout: "templates/layouts/admin.html",
			page:   "templates/pages/admin_audit_logs.html",
			files:  []string{"templates/partials/admin_header.html", "templates/partials/admin_sidebar.html"},
		},
	}

	result := make(map[string]*template.Template, len(pages))
	for name, page := range pages {
		sharedPartials := []string{
			page.layout,
			"templates/partials/icons.html",
			"templates/partials/sidebar_account.html",
			"templates/partials/admin_sidebar_navigation.html",
			"templates/partials/database_sidebar_entities.html",
			"templates/partials/database_sidebar_workspace.html",
		}
		files := append(sharedPartials, page.files...)
		files = append(files, page.page)
		tmpl, err := template.New(name).ParseFS(frontendFiles, files...)
		if err != nil {
			return nil, fmt.Errorf("parsing frontend template %s: %w", name, err)
		}
		result[name] = tmpl
	}
	return result, nil
}

func (h *Handler) StaticHandler() (http.Handler, error) {
	staticFS, err := fs.Sub(frontendFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("opening frontend static files: %w", err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))), nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) routes() {
	r := h.router
	r.Get("/", h.root)
	r.Get("/login", h.loginGet)
	r.Post("/login", h.loginPost)
	r.Post("/logout", h.withSession(h.logout))

	r.Get("/databases", h.withSession(h.listDatabases))
	r.Get("/databases/{databaseID}", h.withSession(h.workspace))
	r.Get("/databases/{databaseID}/schemas/{schema}", h.withSession(h.schema))
	r.Get("/databases/{databaseID}/query", h.withSession(h.queryGet))
	r.Post("/databases/{databaseID}/query", h.withSession(h.queryPost))
	r.Get("/databases/{databaseID}/migrations", h.withSession(h.migrationHistory))
	r.Get("/databases/{databaseID}/migrations/new", h.withSession(h.migrationForm))
	r.Post("/databases/{databaseID}/migrations", h.withSession(h.createMigration))
	r.Get("/databases/{databaseID}/migrations/drafts/{draftID}", h.withSession(h.migrationDraftDetail))
	r.Post("/databases/{databaseID}/migrations/drafts/{draftID}", h.withSession(h.updateMigrationDraft))
	r.Post("/databases/{databaseID}/migrations/drafts/{draftID}/execute", h.withSession(h.executeMigrationDraft))
	r.Get("/databases/{databaseID}/migrations/{version}", h.withSession(h.migrationDetail))

	r.Get("/admin", h.withAdmin(h.adminRoot))
	r.Get("/admin/users", h.withAdmin(h.adminUsers))
	r.Get("/admin/users/new", h.withAdmin(h.adminUserNew))
	r.Get("/admin/users/{username}/edit", h.withAdmin(h.adminUserEdit))
	r.Post("/admin/users/save", h.withAdmin(h.adminUserSave))
	r.Get("/admin/groups", h.withAdmin(h.adminGroups))
	r.Get("/admin/groups/new", h.withAdmin(h.adminGroupNew))
	r.Get("/admin/groups/{groupID}", h.withAdmin(h.adminGroupDetail))
	r.Post("/admin/groups/save", h.withAdmin(h.adminGroupSave))
	r.Get("/admin/databases", h.withAdmin(h.adminDatabases))
	r.Get("/admin/databases/new", h.withAdmin(h.adminDatabaseNew))
	r.Get("/admin/databases/{databaseID}/edit", h.withAdmin(h.adminDatabaseEdit))
	r.Post("/admin/databases/save", h.withAdmin(h.adminDatabaseSave))
	r.Post("/admin/databases/test", h.withAdmin(h.adminDatabaseTest))
	r.Get("/admin/audit-logs", h.withAdmin(h.adminAuditLogs))
}

func (h *Handler) root(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(r)
	if !ok {
		h.redirect(w, r, "/login")
		return
	}
	if session.User.IsAdmin {
		h.redirect(w, r, "/admin/users")
		return
	}
	h.redirect(w, r, "/databases")
}

func (h *Handler) loginGet(w http.ResponseWriter, r *http.Request) {
	if session, ok := h.session(r); ok {
		if session.User.IsAdmin {
			h.redirect(w, r, "/admin/users")
		} else {
			h.redirect(w, r, "/databases")
		}
		return
	}

	data := loginPageData{Title: "Sign in · BScheme", FieldErrors: map[string]string{}}
	switch r.URL.Query().Get("state") {
	case "expired":
		data.SessionExpired = true
	case "invalid":
		data.Error = "Invalid username or password."
	}
	h.render(w, "login", "public-layout", data, http.StatusOK)
}

func (h *Handler) loginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	data := loginPageData{
		Title:       "Sign in · BScheme",
		Username:    username,
		FieldErrors: map[string]string{},
	}
	if username == "" {
		data.FieldErrors["username"] = "Enter your username."
	}
	if password == "" {
		data.FieldErrors["password"] = "Enter your password."
	}
	if len(data.FieldErrors) > 0 {
		h.render(w, "login", "public-layout", data, http.StatusUnprocessableEntity)
		return
	}

	result, err := h.deps.Auth.Login(r.Context(), username, password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrUserDisabled) {
			data.Error = "Invalid username or password."
			h.render(w, "login", "public-layout", data, http.StatusUnauthorized)
			return
		}
		h.renderError(w, http.StatusInternalServerError, "Unable to sign in")
		return
	}

	middleware.SetSessionCookie(w, result.Token, result.ExpiresAt, h.deps.CookieOptions)
	h.setCSRFCookie(w, result.CSRFToken, result.ExpiresAt)
	if result.User.IsAdmin {
		h.redirect(w, r, "/admin/users")
	} else {
		h.redirect(w, r, "/databases")
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request, session pageSession) {
	if !h.validateCSRF(w, r, session) {
		return
	}
	if err := h.deps.Auth.Logout(r.Context(), session.Token); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Unable to log out")
		return
	}
	middleware.ClearSessionCookie(w, h.deps.CookieOptions)
	h.clearCSRFCookie(w)
	h.redirect(w, r, "/login")
}

func (h *Handler) session(r *http.Request) (pageSession, bool) {
	return h.sessionFrom(nil, r)
}

func (h *Handler) sessionFrom(w http.ResponseWriter, r *http.Request) (pageSession, bool) {
	cookie, err := r.Cookie(middleware.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return pageSession{}, false
	}
	validation, err := h.deps.Auth.ValidateSession(r.Context(), cookie.Value)
	if err != nil {
		return pageSession{}, false
	}
	if w != nil && validation.Rotation != nil && validation.Rotation.Token != "" {
		middleware.SetSessionCookie(w, validation.Rotation.Token, validation.Rotation.ExpiresAt, h.deps.CookieOptions)
		cookie.Value = validation.Rotation.Token
	}
	user, err := h.deps.Users.FindByID(r.Context(), validation.AuthenticatedSession.UserID)
	if err != nil || user == nil || user.DisabledAt != nil {
		return pageSession{}, false
	}
	return pageSession{
		Token:     cookie.Value,
		Auth:      validation.AuthenticatedSession,
		User:      user,
		CSRFToken: h.csrfCookie(r),
	}, true
}

func (h *Handler) withSession(next func(http.ResponseWriter, *http.Request, pageSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, ok := h.sessionFrom(w, r)
		if !ok {
			h.redirect(w, r, "/login?state=expired")
			return
		}
		next(w, r, session)
	}
}

func (h *Handler) withAdmin(next func(http.ResponseWriter, *http.Request, pageSession)) http.HandlerFunc {
	return h.withSession(func(w http.ResponseWriter, r *http.Request, session pageSession) {
		isAdmin, err := h.deps.Authorization.IsAdmin(r.Context(), session.User.ID)
		if err != nil {
			h.renderError(w, http.StatusInternalServerError, "Unable to check administrator access")
			return
		}
		if !isAdmin {
			h.renderError(w, http.StatusForbidden, "Platform Admin access required")
			return
		}
		next(w, r, session)
	})
}

func (h *Handler) validateCSRF(w http.ResponseWriter, r *http.Request, session pageSession) bool {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form")
		return false
	}
	if err := h.deps.Auth.ValidateCSRFToken(r.Context(), session.Auth.SessionID, r.FormValue("csrf_token")); err != nil {
		h.renderError(w, http.StatusForbidden, "Invalid CSRF token")
		return false
	}
	return true
}

func (h *Handler) setCSRFCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	options := h.deps.CookieOptions
	if options.Path == "" {
		options.Path = "/"
	}
	if options.SameSite == http.SameSiteDefaultMode {
		options.SameSite = http.SameSiteLaxMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     options.Path,
		Expires:  expiresAt.UTC(),
		Secure:   options.Secure,
		SameSite: options.SameSite,
	})
}

func (h *Handler) clearCSRFCookie(w http.ResponseWriter) {
	options := h.deps.CookieOptions
	if options.Path == "" {
		options.Path = "/"
	}
	http.SetCookie(w, &http.Cookie{Name: CSRFCookieName, Value: "", Path: options.Path, MaxAge: -1, Expires: time.Unix(1, 0).UTC(), Secure: options.Secure, SameSite: options.SameSite})
}

func (h *Handler) csrfCookie(r *http.Request) string {
	cookie, err := r.Cookie(CSRFCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func (h *Handler) basePage(session pageSession) pageData {
	return pageData{
		Mode:      "live",
		CSRFToken: session.CSRFToken,
		CurrentUser: currentUserView{
			ID:       session.User.ID,
			Username: session.User.Username,
			IsAdmin:  session.User.IsAdmin,
		},
	}
}

func (h *Handler) baseAdmin(session pageSession, section string) adminPageData {
	return adminPageData{
		ActiveAdminSection: section,
		CurrentUser: currentUserView{
			ID:       session.User.ID,
			Username: session.User.Username,
			IsAdmin:  session.User.IsAdmin,
		},
		CSRFToken: session.CSRFToken,
	}
}

func (h *Handler) render(w http.ResponseWriter, name, layout string, data any, status int) {
	if w == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.templates[name].ExecuteTemplate(w, layout, data); err != nil {
		// The headers may already be written, so only return a simple body here.
		return
	}
}

func (h *Handler) renderError(w http.ResponseWriter, status int, message string) {
	if w == nil {
		return
	}
	http.Error(w, message, status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (h *Handler) redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, BasePath+path, http.StatusSeeOther)
}

func (h *Handler) parseID(r *http.Request, key string) (uint, error) {
	value, err := strconv.ParseUint(chi.URLParam(r, key), 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return uint(value), nil
}

func (h *Handler) databaseContext(r *http.Request, session pageSession) (*manageddb.ManagedDatabase, access.PermissionLevel, error) {
	dbID, err := h.parseID(r, "databaseID")
	if err != nil {
		return nil, access.PermissionNone, err
	}
	db, err := h.deps.ManagedDatabase.Get(r.Context(), dbID)
	if err != nil {
		return nil, access.PermissionNone, err
	}
	permission, err := h.deps.Authorization.EffectivePermission(r.Context(), session.User.ID, dbID)
	if err != nil {
		return nil, access.PermissionNone, err
	}
	if permission == access.PermissionNone {
		return nil, permission, access.ErrInsufficientPermission
	}
	return db, permission, nil
}

func (h *Handler) databaseView(db *manageddb.ManagedDatabase, permission access.PermissionLevel) databaseView {
	label := "View Only"
	canEdit := false
	if permission == access.PermissionViewEdit {
		label = "View + Edit"
		canEdit = true
	}
	return databaseView{
		ID:          db.ID,
		Name:        db.Name,
		Host:        db.Host,
		Port:        db.Port,
		Database:    db.DBName,
		Username:    db.Username,
		SSLMode:     displaySSLMode(db.SSLMode),
		Status:      "Configured",
		StatusClass: "status-success",
		CanEdit:     canEdit,
		Permission:  label,
		HasPassword: len(db.EncryptedPassword) > 0,
	}
}

func displaySSLMode(mode string) string {
	switch strings.ToLower(mode) {
	case "require":
		return "Require"
	case "disable":
		return "Disable"
	default:
		return "Prefer"
	}
}

func (h *Handler) staticPath(path string) string {
	return "/app" + path
}

func (h *Handler) queryValue(r *http.Request, key, fallback string) string {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	return value
}

func (h *Handler) encodeQuery(values url.Values) string {
	return values.Encode()
}
