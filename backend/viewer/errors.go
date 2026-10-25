package viewer

import "errors"

var (
	ErrInvalidInput   = errors.New("viewer: invalid input")
	ErrReadOnlyQuery  = errors.New("viewer: query must be a SELECT or WITH statement")
	ErrSchemaNotFound = errors.New("viewer: schema not found")
	ErrTableNotFound  = errors.New("viewer: table not found")
)
