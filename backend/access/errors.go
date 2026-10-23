package access

import "errors"

var (
	ErrInvalidGroupName   = errors.New("invalid group name")
	ErrGroupAlreadyExists = errors.New("group already exists")
	ErrGroupNotFound      = errors.New("group not found")
	ErrMembershipExists   = errors.New("membership already exists")
	ErrMembershipNotFound = errors.New("membership not found")
)
