//go:build integration

package frontend_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"golang.org/x/crypto/bcrypt"

	"tubes-wbd/backend/access"
	"tubes-wbd/backend/auth"
	"tubes-wbd/backend/database/platformdb"
	"tubes-wbd/backend/database/targetdb"
	"tubes-wbd/backend/manageddb"
	"tubes-wbd/backend/migration"
	"tubes-wbd/backend/viewer"
	"tubes-wbd/frontend"
)

const (
	pgUser     = "platform"
	pgPassword = "platform_password"
)

var csrfInputPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

// flowEnv is the whole application wired as in cmd/server, backed by one
// PostgreSQL container that hosts both the Platform DB and the target DB.
type flowEnv struct {
	server         *httptest.Server
	target         *pgx.Conn
	databaseID     uint
	userManagement *auth.UserManagementService
	users          map[string]*auth.User
}

func newFlowEnv(t *testing.T) *flowEnv {
	t.Helper()
	ctx := context.Background()

	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("platform"),
		tcpostgres.WithUsername(pgUser),
		tcpostgres.WithPassword(pgPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	require.NoError(t, err)

	host, err := ctr.Host(ctx)
	require.NoError(t, err)
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	dsn := func(dbName string) string {
		return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
			pgUser, pgPassword, net.JoinHostPort(host, strconv.Itoa(int(port.Num()))), dbName)
	}

	platformConn, err := pgx.Connect(ctx, dsn("platform"))
	require.NoError(t, err)
	_, err = platformConn.Exec(ctx, "CREATE DATABASE shop")
	require.NoError(t, err)
	require.NoError(t, platformConn.Close(ctx))

	target, err := pgx.Connect(ctx, dsn("shop"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = target.Close(context.Background()) })
	_, err = target.Exec(ctx, `
		CREATE TABLE public.customers (id serial PRIMARY KEY, name text NOT NULL);
		INSERT INTO public.customers (name) VALUES ('Alice'), ('Bob');
	`)
	require.NoError(t, err)

	db, err := platformdb.Open(ctx, platformdb.Config{DSN: dsn("platform")})
	require.NoError(t, err)
	t.Cleanup(func() { _ = platformdb.Close(db) })
	require.NoError(t, auth.AutoMigrate(db))
	require.NoError(t, manageddb.AutoMigrate(db))
	require.NoError(t, access.AutoMigrate(db))
	require.NoError(t, migration.AutoMigrate(db))

	userRepository := auth.NewGormUserRepository(db)
	sessionRepository := auth.NewGormSessionRepository(db)
	hasher := auth.NewBcryptHasher(bcrypt.MinCost)
	authService, err := auth.NewAuthService(userRepository, sessionRepository, hasher, auth.ServiceConfig{})
	require.NoError(t, err)
	accessRepository := access.NewGormRepository(db)
	authorization, err := access.NewAuthorizationService(userRepository, accessRepository)
	require.NoError(t, err)
	userManagement, err := auth.NewUserManagementService(userRepository, sessionRepository, hasher)
	require.NoError(t, err)
	groups, err := access.NewGroupService(userRepository, accessRepository)
	require.NoError(t, err)

	cipher, err := manageddb.NewCipher([]byte(strings.Repeat("k", manageddb.KeySize)))
	require.NoError(t, err)
	managedRepository := manageddb.NewGormRepository(db)
	source, err := manageddb.NewSource(managedRepository, cipher)
	require.NoError(t, err)
	targetManager := targetdb.NewManager(source, targetdb.Options{})
	t.Cleanup(targetManager.Close)
	managedDatabases, err := manageddb.NewManagedDBService(managedRepository, cipher, targetManager)
	require.NoError(t, err)
	viewerService, err := viewer.NewViewerService(authorization, targetManager, viewer.Config{
		DefaultPageSize: 50,
		MaxPageSize:     200,
	})
	require.NoError(t, err)
	migrations, err := migration.NewMigrationService(
		authorization,
		targetManager,
		migration.NewGormRepository(db),
		migration.Options{},
	)
	require.NoError(t, err)

	handler, err := frontend.NewHandler(frontend.Dependencies{
		Auth:            authService,
		Users:           userRepository,
		UserManagement:  userManagement,
		Groups:          groups,
		Authorization:   authorization,
		ManagedDatabase: managedDatabases,
		Viewer:          viewerService,
		Migrations:      migrations,
	})
	require.NoError(t, err)
	router := chi.NewRouter()
	router.Mount(frontend.BasePath, handler)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	// Platform Admin setup: one target database, an editor group with View +
	// Edit, a viewer group with View Only, and an outsider in no group.
	shop, err := managedDatabases.Create(ctx, manageddb.Input{
		Name:     "shop",
		Host:     host,
		Port:     port.Num(),
		Username: pgUser,
		Password: pgPassword,
		DBName:   "shop",
		SSLMode:  "disable",
	})
	require.NoError(t, err)

	env := &flowEnv{
		server:         server,
		target:         target,
		databaseID:     shop.ID,
		userManagement: userManagement,
		users:          map[string]*auth.User{},
	}
	for _, username := range []string{"editor", "viewer", "outsider"} {
		user, err := userManagement.CreateUser(ctx, username, username+"-password")
		require.NoError(t, err)
		env.users[username] = user
	}
	for groupName, member := range map[string]struct {
		username   string
		permission access.PermissionLevel
	}{
		"editors": {"editor", access.PermissionViewEdit},
		"viewers": {"viewer", access.PermissionViewOnly},
	} {
		group, err := groups.CreateGroup(ctx, groupName)
		require.NoError(t, err)
		require.NoError(t, groups.AddMember(ctx, group.ID, env.users[member.username].ID))
		_, err = groups.SetDatabasePermission(ctx, group.ID, shop.ID, member.permission)
		require.NoError(t, err)
	}

	return env
}

func (e *flowEnv) dbPath(suffix string) string {
	return fmt.Sprintf("/app/databases/%d", e.databaseID) + suffix
}

func (e *flowEnv) columnExists(t *testing.T, column string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, e.target.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'customers' AND column_name = $1)`,
		column).Scan(&exists))
	return exists
}

func (e *flowEnv) historyVersions(t *testing.T) []int64 {
	t.Helper()
	rows, err := e.target.Query(context.Background(), "SELECT version FROM _dbm.migration_history ORDER BY version")
	require.NoError(t, err)
	versions, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	require.NoError(t, err)
	return versions
}

// browser is an HTTP client with a cookie jar that follows redirects, like a
// user's browser session.
type browser struct {
	t       *testing.T
	client  *http.Client
	baseURL string
}

type page struct {
	status int
	path   string
	query  url.Values
	body   string
}

func (e *flowEnv) newBrowser(t *testing.T) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &browser{t: t, client: &http.Client{Jar: jar}, baseURL: e.server.URL}
}

func (b *browser) get(path string) page {
	b.t.Helper()
	response, err := b.client.Get(b.baseURL + path)
	require.NoError(b.t, err)
	return readPage(b.t, response)
}

func (b *browser) post(path string, form url.Values) page {
	b.t.Helper()
	response, err := b.client.PostForm(b.baseURL+path, form)
	require.NoError(b.t, err)
	return readPage(b.t, response)
}

// submit posts a form with the CSRF token taken from a page, as the browser
// would when the user submits a form rendered on that page.
func (b *browser) submit(from page, path string, form url.Values) page {
	b.t.Helper()
	form.Set("csrf_token", csrfToken(b.t, from))
	return b.post(path, form)
}

func (b *browser) login(username string) page {
	b.t.Helper()
	loginPage := b.get("/app/login")
	require.Equal(b.t, http.StatusOK, loginPage.status)
	return b.submit(loginPage, "/app/login", url.Values{
		"username": {username},
		"password": {username + "-password"},
	})
}

func readPage(t *testing.T, response *http.Response) page {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return page{
		status: response.StatusCode,
		path:   response.Request.URL.Path,
		query:  response.Request.URL.Query(),
		body:   string(body),
	}
}

func csrfToken(t *testing.T, from page) string {
	t.Helper()
	match := csrfInputPattern.FindStringSubmatch(from.body)
	require.Len(t, match, 2, "page %s has no csrf_token input", from.path)
	return match[1]
}

func TestIntegrationEditorFlowFromLoginToMigrationHistory(t *testing.T) {
	env := newFlowEnv(t)
	editor := env.newBrowser(t)

	// Login → database list.
	databases := editor.login("editor")
	require.Equal(t, http.StatusOK, databases.status)
	require.Equal(t, "/app/databases", databases.path)
	require.Contains(t, databases.body, "<h2>shop</h2>")
	require.Contains(t, databases.body, `href="`+env.dbPath("?mode="))

	// Select database → schema, table structure, and data.
	workspace := editor.get(env.dbPath(""))
	require.Equal(t, http.StatusOK, workspace.status)
	require.Contains(t, workspace.body, "customers")
	require.Contains(t, workspace.body, "Alice")
	require.Contains(t, workspace.body, `<span class="badge badge-permission">View &#43; Edit</span>`)

	schema := editor.get(env.dbPath("/schemas/public"))
	require.Equal(t, http.StatusOK, schema.status)
	require.Contains(t, schema.body, "customers")

	queryPage := editor.get(env.dbPath("/query"))
	require.Equal(t, http.StatusOK, queryPage.status)
	require.Contains(t, queryPage.body, `href="`+env.dbPath("/migrations/new"))
	queryResult := editor.submit(queryPage, env.dbPath("/query"), url.Values{
		"action": {"run"},
		"sql":    {"SELECT name FROM public.customers ORDER BY id"},
	})
	require.Equal(t, http.StatusOK, queryResult.status)
	require.Contains(t, queryResult.body, "<td>Alice</td>")
	require.Contains(t, queryResult.body, "<td>Bob</td>")

	// Create migration → draft is saved but not applied yet.
	form := editor.get(env.dbPath("/migrations/new"))
	require.Equal(t, http.StatusOK, form.status)
	draft := editor.submit(form, env.dbPath("/migrations"), url.Values{
		"name": {"Add phone"},
		"sql":  {"ALTER TABLE public.customers ADD COLUMN phone text"},
	})
	require.Equal(t, http.StatusOK, draft.status)
	require.True(t, draft.query.Has("saved"))
	require.Contains(t, draft.body, "Migration draft saved.")
	require.Contains(t, draft.body, ">draft<")
	require.False(t, env.columnExists(t, "phone"))
	draftPath := draft.path

	// Execute → committed to the target database.
	executed := editor.submit(draft, draftPath+"/execute", url.Values{})
	require.Equal(t, http.StatusOK, executed.status)
	require.Equal(t, draftPath, executed.path)
	require.True(t, executed.query.Has("executed"))
	require.Contains(t, executed.body, "Migration draft executed.")
	require.Contains(t, executed.body, ">applied<")
	require.NotContains(t, executed.body, draftPath+`/execute"`, "an applied draft cannot be executed again")
	require.True(t, env.columnExists(t, "phone"))
	require.Equal(t, []int64{1}, env.historyVersions(t))

	// Migration history shows version, name, and executor.
	history := editor.get(env.dbPath("/migrations"))
	require.Equal(t, http.StatusOK, history.status)
	require.Contains(t, history.body, `<span class="cell-code">1</span><strong>Add phone</strong><span>editor</span>`)

	detail := editor.get(env.dbPath("/migrations/1"))
	require.Equal(t, http.StatusOK, detail.status)
	require.Contains(t, detail.body, "ALTER TABLE public.customers ADD COLUMN phone text")
	require.Contains(t, detail.body, "editor")
}

func TestIntegrationFailedMigrationRollsBackThroughUI(t *testing.T) {
	env := newFlowEnv(t)
	editor := env.newBrowser(t)
	editor.login("editor")

	// The first statement is valid, the second fails: neither may be applied.
	form := editor.get(env.dbPath("/migrations/new"))
	draft := editor.submit(form, env.dbPath("/migrations"), url.Values{
		"name": {"Add email and break"},
		"sql": {"ALTER TABLE public.customers ADD COLUMN email text;\n" +
			"ALTER TABLE public.missing_table ADD COLUMN x int;"},
	})
	require.Equal(t, http.StatusOK, draft.status)

	failed := editor.submit(draft, draft.path+"/execute", url.Values{})

	require.Equal(t, http.StatusUnprocessableEntity, failed.status)
	require.Contains(t, failed.body, ">failed<")
	require.Contains(t, failed.body, "missing_table")
	require.Contains(t, failed.body, "migration-run-error")
	require.False(t, env.columnExists(t, "email"), "the first statement must be rolled back")

	history := editor.get(env.dbPath("/migrations"))
	require.Equal(t, http.StatusOK, history.status)
	require.NotContains(t, history.body, "<strong>Add email and break</strong><span>editor</span>",
		"a failed migration must not appear in the applied history")

	// A failed draft stays editable: fix it and execute again.
	fixed := editor.submit(failed, draft.path, url.Values{
		"name": {"Add email"},
		"sql":  {"ALTER TABLE public.customers ADD COLUMN email text"},
	})
	require.Equal(t, http.StatusOK, fixed.status)
	retried := editor.submit(fixed, draft.path+"/execute", url.Values{})
	require.Equal(t, http.StatusOK, retried.status)
	require.Contains(t, retried.body, ">applied<")
	require.True(t, env.columnExists(t, "email"))
	require.Equal(t, []int64{1}, env.historyVersions(t))
}

func TestIntegrationViewOnlyCanReadButNotChangeThroughUI(t *testing.T) {
	env := newFlowEnv(t)

	// The editor prepares a draft the viewer will try to execute.
	editor := env.newBrowser(t)
	editor.login("editor")
	form := editor.get(env.dbPath("/migrations/new"))
	draft := editor.submit(form, env.dbPath("/migrations"), url.Values{
		"name": {"Add phone"},
		"sql":  {"ALTER TABLE public.customers ADD COLUMN phone text"},
	})
	require.Equal(t, http.StatusOK, draft.status)

	viewOnly := env.newBrowser(t)
	databases := viewOnly.login("viewer")
	require.Equal(t, http.StatusOK, databases.status)
	require.Contains(t, databases.body, "<h2>shop</h2>")

	workspace := viewOnly.get(env.dbPath(""))
	require.Equal(t, http.StatusOK, workspace.status)
	require.Contains(t, workspace.body, "Alice")
	require.Contains(t, workspace.body, `<span class="badge badge-permission">View Only</span>`)

	// Read-only queries are allowed; data changes are not.
	queryPage := viewOnly.get(env.dbPath("/query"))
	require.Equal(t, http.StatusOK, queryPage.status)
	require.NotContains(t, queryPage.body, env.dbPath("/migrations/new"))
	selected := viewOnly.submit(queryPage, env.dbPath("/query"), url.Values{
		"action": {"run"},
		"sql":    {"SELECT name FROM public.customers ORDER BY id"},
	})
	require.Equal(t, http.StatusOK, selected.status)
	require.Contains(t, selected.body, "<td>Alice</td>")
	deleted := viewOnly.submit(queryPage, env.dbPath("/query"), url.Values{
		"action": {"run"},
		"sql":    {"DELETE FROM public.customers"},
	})
	require.Equal(t, http.StatusUnprocessableEntity, deleted.status)
	var customers int
	require.NoError(t, env.target.QueryRow(context.Background(), "SELECT count(*) FROM public.customers").Scan(&customers))
	require.Equal(t, 2, customers)

	// History and draft details are readable, without edit or execute forms.
	history := viewOnly.get(env.dbPath("/migrations"))
	require.Equal(t, http.StatusOK, history.status)
	require.NotContains(t, history.body, env.dbPath("/migrations/new"))
	viewerDraft := viewOnly.get(draft.path)
	require.Equal(t, http.StatusOK, viewerDraft.status)
	require.Contains(t, viewerDraft.body, "Add phone")
	require.NotContains(t, viewerDraft.body, draft.path+`/execute"`)

	// The backend rejects writes even when the forms are posted directly.
	created := viewOnly.submit(queryPage, env.dbPath("/migrations"), url.Values{
		"name": {"Viewer migration"},
		"sql":  {"ALTER TABLE public.customers ADD COLUMN hacked text"},
	})
	require.Equal(t, http.StatusForbidden, created.status)
	executed := viewOnly.submit(queryPage, draft.path+"/execute", url.Values{})
	require.Equal(t, http.StatusForbidden, executed.status)
	require.False(t, env.columnExists(t, "phone"))
	require.False(t, env.columnExists(t, "hacked"))

	// Disabling the user revokes the active session immediately.
	_, err := env.userManagement.SetDisabled(context.Background(), env.users["viewer"].ID, true)
	require.NoError(t, err)
	afterDisable := viewOnly.get("/app/databases")
	require.Equal(t, "/app/login", afterDisable.path)
	require.Equal(t, "expired", afterDisable.query.Get("state"))
}

func TestIntegrationUserWithoutGroupCannotSeeDatabase(t *testing.T) {
	env := newFlowEnv(t)
	outsider := env.newBrowser(t)

	databases := outsider.login("outsider")
	require.Equal(t, http.StatusOK, databases.status)
	require.NotContains(t, databases.body, "<h2>shop</h2>")

	for _, path := range []string{"", "/query", "/migrations", "/schemas/public"} {
		response := outsider.get(env.dbPath(path))
		require.Equal(t, http.StatusForbidden, response.status, "GET %s", env.dbPath(path))
		require.NotContains(t, response.body, "Alice")
	}
}
