package migration

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidInput        = errors.New("migration: invalid input")
	ErrTransactionControl  = errors.New("migration: transaction control statements are not allowed")
	ErrMigrationInProgress = errors.New("migration: another migration is running on this database")
	ErrExecutionFailed     = errors.New("migration: execution failed")
	ErrVersionNotFound     = errors.New("migration: version not found")
	ErrDraftNotFound       = errors.New("migration: draft not found")
	ErrDraftNotEditable    = errors.New("migration: draft cannot be edited while running or after it is applied")
	ErrDraftNotExecutable  = errors.New("migration: draft cannot be executed while running or after it is applied")
)

type ExecutionError struct {
	Code     string
	Message  string
	Detail   string
	Hint     string
	Position int32
}

func (e *ExecutionError) Error() string {
	return fmt.Sprintf("%s: %s (SQLSTATE %s)", ErrExecutionFailed, e.Message, e.Code)
}

func (e *ExecutionError) Is(target error) bool { return target == ErrExecutionFailed }