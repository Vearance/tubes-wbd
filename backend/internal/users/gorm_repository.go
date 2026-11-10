package users

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

type GormRepository struct {
	db *gorm.DB
}

var _ Repository = (*GormRepository)(nil)

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) FindByUsername(
	ctx context.Context,
	username string,
) (*User, error) {
	var user User
	result := r.db.WithContext(ctx).
		Where("username = ?", strings.TrimSpace(username)).
		Limit(1).
		Find(&user)
	if result.Error != nil {
		return nil, fmt.Errorf("querying user by username: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

func (r *GormRepository) FindByID(ctx context.Context, userID uint) (*User, error) {
	var user User
	result := r.db.WithContext(ctx).
		Where("id = ?", userID).
		Limit(1).
		Find(&user)
	if result.Error != nil {
		return nil, fmt.Errorf("querying user by id: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

func (r *GormRepository) FindAdmin(ctx context.Context) (*User, error) {
	var user User
	result := r.db.WithContext(ctx).
		Where("is_admin = ?", true).
		Limit(1).
		Find(&user)
	if result.Error != nil {
		return nil, fmt.Errorf("querying platform admin: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrUserNotFound
	}
	return &user, nil
}

func (r *GormRepository) List(ctx context.Context) ([]User, error) {
	var users []User
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return users, nil
}

func (r *GormRepository) Create(ctx context.Context, user *User) error {
	user.Username = strings.TrimSpace(user.Username)
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	return nil
}

func (r *GormRepository) Update(ctx context.Context, user *User) error {
	user.Username = strings.TrimSpace(user.Username)
	if err := r.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("updating user: %w", err)
	}
	return nil
}
