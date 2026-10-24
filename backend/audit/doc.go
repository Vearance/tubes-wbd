// Package audit provides an append-only platform audit record and bounded
// queries. It is not wired into the HTTP server or authentication flow yet.
// Database write restrictions, retention, and transactional recording of
// admin changes belong to the later integration step.
package audit
