package frontend

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
)

func TestParseTemplates(t *testing.T) {
	if _, err := parseTemplates(); err != nil {
		t.Fatalf("parse frontend templates: %v", err)
	}
}

func TestQueryTemplateRendersReadOnlyActions(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse frontend templates: %v", err)
	}

	data := pageData{
		Mode:           "live",
		CSRFToken:      "csrf-token",
		ActiveDatabase: &databaseView{ID: 7, Name: "targetdb"},
		QuerySQL:       "SELECT 1",
		QueryExecuted:  true,
		QueryColumns:   []string{"result"},
		QueryRows: []tableRowView{{Cells: []tableCellView{{
			Value: "1",
		}}}},
	}
	markup := executeTemplate(t, templates["query"], "app-layout", data)

	assertContains(t, markup, `action="/app/databases/7/query"`)
	assertContains(t, markup, `name="action" value="explain"`)
	assertContains(t, markup, `name="action" value="run"`)
	assertContains(t, markup, `name="csrf_token" value="csrf-token"`)
	assertContains(t, markup, "<th>result</th>")
	assertContains(t, markup, "<td>1</td>")
}

func TestMigrationDraftTemplateRendersEditAndExecuteActions(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("parse frontend templates: %v", err)
	}

	data := pageData{
		Mode:           "live",
		CanEdit:        true,
		CSRFToken:      "csrf-token",
		ActiveDatabase: &databaseView{ID: 7, Name: "targetdb"},
		MigrationDraft: &migrationDraftView{
			ID:          11,
			Name:        "Add phone",
			SQL:         "ALTER TABLE customers ADD COLUMN phone TEXT;",
			Status:      "draft",
			StatusClass: "status-info",
			CanEdit:     true,
			CanExecute:  true,
		},
	}
	markup := executeTemplate(t, templates["migration-draft"], "app-layout", data)

	assertContains(t, markup, `action="/app/databases/7/migrations/drafts/11"`)
	assertContains(t, markup, `action="/app/databases/7/migrations/drafts/11/execute"`)
	assertContains(t, markup, `name="csrf_token" value="csrf-token"`)
	assertContains(t, markup, "Add phone")
}

func executeTemplate(t *testing.T, parsed *template.Template, name string, data any) string {
	t.Helper()

	var output bytes.Buffer
	if err := parsed.ExecuteTemplate(&output, name, data); err != nil {
		t.Fatalf("execute %s template: %v", name, err)
	}
	return output.String()
}

func assertContains(t *testing.T, value, fragment string) {
	t.Helper()
	if !strings.Contains(value, fragment) {
		t.Fatalf("expected output to contain %q", fragment)
	}
}
