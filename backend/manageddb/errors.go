package manageddb

import "errors"

var (
	ErrNotFound      = errors.New("manageddb: database not found")
	ErrDuplicateName = errors.New("manageddb: database name already exists")
	ErrInvalidInput  = errors.New("manageddb: invalid input")
)