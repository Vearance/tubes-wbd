package viewer

import "errors"

var (
	ErrInvalidInput   = errors.New("viewer: invalid input")
	ErrSchemaNotFound = errors.New("viewer: schema not found")
	ErrTableNotFound  = errors.New("viewer: table not found")
)