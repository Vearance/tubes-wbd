package frontend

import (
	"strings"
	"testing"

	"tubes-wbd/backend/viewer"
)

type pageCase struct {
	layout   string
	data     any
	contains []string
}

func sampleDatabase() *databaseView {
	return &databaseView{ID: 7, Name: "shop", Status: "Configured", StatusClass: "status-success"}
}

func sampleWorkspace(canEdit bool) pageData {
	label := "View Only"
	if canEdit {
		label = "View + Edit"
	}
	return pageData{
		Mode:            "live",
		CanEdit:         canEdit,
		PermissionLabel: label,
		CurrentUser:     currentUserView{ID: 3, Username: "alice"},
		CSRFToken:       "csrf-token",
		ActiveDatabase:  sampleDatabase(),
		Schemas:         []schemaView{{Name: "public", Tables: []string{"customers"}}},
		ActiveSchema:    "public",
		ActiveTable:     "customers",
		ActiveTab:       "data",
	}
}

func samplePageCases() map[string]pageCase {
	defaultText := "''::text"

	workspace := sampleWorkspace(true)
	workspace.ActiveSection = "table"
	workspace.TableDetail = &viewer.TableDetail{
		Columns: []viewer.Column{
			{Name: "id", Type: "integer"},
			{Name: "name", Type: "text", Nullable: true, Default: &defaultText},
		},
	}
	workspace.TableRows = []tableRowView{
		{Cells: []tableCellView{{Value: "1"}, {Value: "Alice"}}},
		{Cells: []tableCellView{{Value: "2"}, {Null: true}}},
	}

	schema := sampleWorkspace(true)
	schema.ActiveSection = "schema"
	schema.SchemaTables = []schemaTableView{{Name: "customers", Columns: 2, Constraints: 1, Indexes: 1}}

	history := sampleWorkspace(true)
	history.ActiveSection = "migrations"
	history.Migrations = []migrationView{{ID: 1, Version: 1, Name: "Add phone", Executor: "alice", Status: "Applied", StatusClass: "status-success"}}
	history.Drafts = []migrationDraftSummaryView{{ID: 11, Name: "Add email", Status: "draft", StatusClass: "status-inactive"}}

	form := sampleWorkspace(true)
	form.ActiveSection = "migrations"

	detail := sampleWorkspace(true)
	detail.ActiveSection = "migrations"
	detail.Migration = migrationView{ID: 1, Version: 1, Name: "Add phone", Executor: "alice", Status: "Applied", SQL: "ALTER TABLE public.customers ADD COLUMN phone text"}

	draft := sampleWorkspace(true)
	draft.ActiveSection = "migrations"
	draft.MigrationDraft = &migrationDraftView{ID: 11, Name: "Add email", SQL: "SELECT 1", Status: "failed", StatusClass: "status-danger", AppliedVersion: "—", CanEdit: true, CanExecute: true}
	draft.MigrationRuns = []migrationRunView{{Status: "failed", StatusClass: "status-danger", Version: "—", ExecutedBy: "alice", ErrorMessage: "relation does not exist"}}

	query := sampleWorkspace(true)
	query.ActiveSection = "query"
	query.QuerySQL = "SELECT 1"

	admin := func(section string) adminPageData {
		return adminPageData{
			ActiveAdminSection: section,
			CurrentUser:        currentUserView{ID: 1, Username: "admin", IsAdmin: true},
			CSRFToken:          "csrf-token",
		}
	}
	users := admin("users")
	users.Users = []adminUserView{{ID: 3, Username: "alice", Role: "User", Status: "Active", StatusClass: "status-success", GroupNames: []adminUserGroupView{{ID: 2, Name: "editors"}}}}
	userForm := admin("users")
	userForm.Editing = true
	userForm.FormUsername = "alice"
	groups := admin("groups")
	groups.Groups = []adminGroupView{{ID: 2, Name: "editors", MemberCount: 1, AccessCount: 1}}
	group := admin("groups")
	group.GroupID = 2
	group.Group = adminGroupDetailView{
		Name:    "editors",
		Members: []adminMemberView{{ID: 3, Username: "alice", Role: "User"}},
		Access:  []adminAccessView{{ID: 1, Database: "shop", DatabaseID: 7, Permission: "View + Edit"}},
	}
	group.AvailableUsers = []adminUserView{{ID: 3, Username: "alice"}}
	group.AvailableDatabases = []databaseView{*sampleDatabase()}
	databases := admin("databases")
	databases.Connections = []databaseView{{ID: 7, Name: "shop", Host: "db", Port: 5432, Database: "shop", Status: "Configured", StatusClass: "status-success"}}
	databaseForm := admin("databases")
	databaseForm.Editing = true
	databaseForm.Connection = databaseView{ID: 7, Name: "shop", Host: "db", Port: 5432, Database: "shop", Username: "app", SSLMode: "disable"}

	return map[string]pageCase{
		"login": {
			layout: "public-layout",
			data:   loginPageData{CSRFToken: "csrf-token", Username: "alice", Error: "Invalid username or password.", FieldErrors: map[string]string{}},
			contains: []string{
				`action="/app/login"`,
				`name="csrf_token" value="csrf-token"`,
				`value="alice"`,
				"Invalid username or password.",
			},
		},
		"databases": {
			layout: "app-layout",
			data:   pageData{Mode: "live", CurrentUser: currentUserView{Username: "alice"}, CSRFToken: "csrf-token", Databases: []databaseView{*sampleDatabase()}},
			contains: []string{
				"<h2>shop</h2>",
				`href="/app/databases/7?mode=live"`,
				`action="/app/logout"`,
			},
		},
		"workspace": {
			layout: "app-layout",
			data:   workspace,
			contains: []string{
				"public.customers",
				"<th>name</th>",
				"<td>Alice</td>",
				`<span class="sql-null">NULL</span>`,
				`href="/app/databases/7/query?mode=live"`,
				`href="/app/databases/7/migrations?mode=live"`,
			},
		},
		"schema": {
			layout: "app-layout",
			data:   schema,
			contains: []string{
				"<strong>customers</strong>",
				`href="/app/databases/7?mode=live&schema=public&table=customers"`,
			},
		},
		"history": {
			layout: "app-layout",
			data:   history,
			contains: []string{
				`href="/app/databases/7/migrations/1?mode=live"`,
				`<strong>Add phone</strong><span>alice</span>`,
				`href="/app/databases/7/migrations/drafts/11?mode=live"`,
				`href="/app/databases/7/migrations/new?mode=live"`,
			},
		},
		"migration-form": {
			layout: "app-layout",
			data:   form,
			contains: []string{
				`action="/app/databases/7/migrations"`,
				`name="csrf_token" value="csrf-token"`,
				`name="name"`,
				`name="sql"`,
			},
		},
		"migration-detail": {
			layout: "app-layout",
			data:   detail,
			contains: []string{
				"<h1>Add phone</h1>",
				"ALTER TABLE public.customers ADD COLUMN phone text",
				"<strong>alice</strong>",
			},
		},
		"migration-draft": {
			layout: "app-layout",
			data:   draft,
			contains: []string{
				`action="/app/databases/7/migrations/drafts/11"`,
				`action="/app/databases/7/migrations/drafts/11/execute"`,
				"relation does not exist",
			},
		},
		"query": {
			layout: "app-layout",
			data:   query,
			contains: []string{
				`action="/app/databases/7/query"`,
				`name="action" value="run"`,
				`name="action" value="explain"`,
			},
		},
		"admin-users": {
			layout: "admin-layout",
			data:   users,
			contains: []string{
				`href="/app/admin/users/new"`,
				`href="/app/admin/users/alice/edit"`,
				`href="/app/admin/groups/2"`,
			},
		},
		"admin-user-form": {
			layout: "admin-layout",
			data:   userForm,
			contains: []string{
				`action="/app/admin/users/save"`,
				`name="csrf_token" value="csrf-token"`,
				`value="alice"`,
			},
		},
		"admin-groups": {
			layout: "admin-layout",
			data:   groups,
			contains: []string{
				`href="/app/admin/groups/new"`,
				`href="/app/admin/groups/2"`,
				"<strong>editors</strong>",
			},
		},
		"admin-group": {
			layout: "admin-layout",
			data:   group,
			contains: []string{
				`action="/app/admin/groups/save"`,
				`name="group_id" value="2"`,
				`name="csrf_token" value="csrf-token"`,
				"shop",
			},
		},
		"admin-databases": {
			layout: "admin-layout",
			data:   databases,
			contains: []string{
				`href="/app/admin/databases/new"`,
				`href="/app/admin/databases/7/edit"`,
				"db:5432",
			},
		},
		"admin-database-form": {
			layout: "admin-layout",
			data:   databaseForm,
			contains: []string{
				`action="/app/admin/databases/save"`,
				`name="connection_id" value="7"`,
				`name="csrf_token" value="csrf-token"`,
			},
		},
	}
}

func TestEveryPageRendersWithItsKeyLinksAndForms(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse frontend templates: %v", err)
	}
	cases := samplePageCases()

	for name := range templates {
		if _, ok := cases[name]; !ok {
			t.Errorf("page %q has no render test case; add one to samplePageCases", name)
		}
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			parsed, ok := templates[name]
			if !ok {
				t.Fatalf("page %q is not registered", name)
			}
			markup := executeTemplate(t, parsed, test.layout, test.data)
			for _, fragment := range test.contains {
				assertContains(t, markup, fragment)
			}
		})
	}
}

func TestViewOnlyPagesHideMigrationWriteActions(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse frontend templates: %v", err)
	}

	query := sampleWorkspace(false)
	query.ActiveSection = "query"
	query.QuerySQL = "SELECT 1"

	history := sampleWorkspace(false)
	history.ActiveSection = "migrations"
	history.Drafts = []migrationDraftSummaryView{{ID: 11, Name: "Add email", Status: "draft"}}

	form := sampleWorkspace(false)
	form.ActiveSection = "migrations"

	draft := sampleWorkspace(false)
	draft.ActiveSection = "migrations"
	draft.MigrationDraft = &migrationDraftView{ID: 11, Name: "Add email", SQL: "SELECT 1", Status: "draft", AppliedVersion: "—"}

	detail := sampleWorkspace(false)
	detail.ActiveSection = "migrations"
	detail.Migration = migrationView{ID: 1, Version: 1, Name: "Add phone", SQL: "SELECT 1"}

	tests := []struct {
		page        string
		data        pageData
		contains    []string
		notContains []string
	}{
		{
			page: "query",
			data: query,
			contains: []string{
				`<span class="badge badge-permission">View Only</span>`,
				`name="action" value="run"`,
				`name="action" value="explain"`,
			},
			notContains: []string{"/migrations/new"},
		},
		{
			page:        "history",
			data:        history,
			contains:    []string{`href="/app/databases/7/migrations/drafts/11?mode=live"`},
			notContains: []string{"/migrations/new"},
		},
		{
			page:        "migration-form",
			data:        form,
			contains:    []string{"View Only access"},
			notContains: []string{`action="/app/databases/7/migrations"`, `name="sql"`},
		},
		{
			page:     "migration-draft",
			data:     draft,
			contains: []string{"Add email"},
			notContains: []string{
				`action="/app/databases/7/migrations/drafts/11"`,
				`action="/app/databases/7/migrations/drafts/11/execute"`,
			},
		},
		{
			page:        "migration-detail",
			data:        detail,
			contains:    []string{"You have View Only access."},
			notContains: []string{"/migrations/new"},
		},
	}

	for _, test := range tests {
		t.Run(test.page, func(t *testing.T) {
			markup := executeTemplate(t, templates[test.page], "app-layout", test.data)
			for _, fragment := range test.contains {
				assertContains(t, markup, fragment)
			}
			for _, fragment := range test.notContains {
				if strings.Contains(markup, fragment) {
					t.Fatalf("View Only page %q must not contain %q", test.page, fragment)
				}
			}
		})
	}
}
