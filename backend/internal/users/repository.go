package users

import "context"

type IdentityRepository interface {
	FindByUsername(ctx context.Context, username string) (*User, error)
	FindByID(ctx context.Context, userID uint) (*User, error)
	FindAdmin(ctx context.Context) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
}

type Repository interface {
	IdentityRepository
	List(ctx context.Context) ([]User, error)
}
