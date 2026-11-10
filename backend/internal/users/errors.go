package users

import "errors"

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidUserInput   = errors.New("invalid user input")
	ErrUserAlreadyExists  = errors.New("user already exists")
	ErrCannotDisableAdmin = errors.New("cannot disable platform admin")
)
