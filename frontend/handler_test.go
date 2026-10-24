package frontend

import "testing"

func TestParseTemplates(t *testing.T) {
	if _, err := parseTemplates(); err != nil {
		t.Fatalf("parse frontend templates: %v", err)
	}
}
