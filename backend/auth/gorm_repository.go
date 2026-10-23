package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type GormUserRepository struct {
	db *gorm.DB
}

var _ UserRepository = (*GormUserRepository)(nil)

func NewGormUserRepository(db *gorm.DB) *GormUserRepository {
	return &GormUserRepository{db: db}
}

func (r *GormUserRepository) FindByUsername(
	ctx context.Context,
	username string,
) (*User, error) {
	var user User
	username = strings.TrimSpace(username)

	err := r.db.WithContext(ctx).
		Where("username = ?", username).
		First(&user).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by username: %w", err)
	}

	return &user, nil
}

func (r *GormUserRepository) FindByID(
	ctx context.Context,
	userID uint,
) (*User, error) {
	var user User

	err := r.db.WithContext(ctx).First(&user, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying user by id: %w", err)
	}

	return &user, nil
}

func (r *GormUserRepository) FindAdmin(ctx context.Context) (*User, error) {
	var user User

	err := r.db.WithContext(ctx).
		Where("is_admin = ?", true).
		First(&user).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying platform admin: %w", err)
	}

	return &user, nil
}

func (r *GormUserRepository) Create(
	ctx context.Context,
	user *User,
) error {
	user.Username = strings.TrimSpace(user.Username)
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	return nil
}

func (r *GormUserRepository) Update(
	ctx context.Context,
	user *User,
) error {
	user.Username = strings.TrimSpace(user.Username)
	if err := r.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

type GormSessionRepository struct {
	db *gorm.DB
}

var _ SessionRepository = (*GormSessionRepository)(nil)

func NewGormSessionRepository(db *gorm.DB) *GormSessionRepository {
	return &GormSessionRepository{db: db}
}

func (r *GormSessionRepository) Create(
	ctx context.Context,
	session *Session,
) error {
	if err := r.db.WithContext(ctx).Create(session).Error; err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	return nil
}

func (r *GormSessionRepository) FindByID(
	ctx context.Context,
	sessionID uint,
) (*Session, error) {
	var session Session

	err := r.db.WithContext(ctx).First(&session, sessionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying session by id: %w", err)
	}

	return &session, nil
}

func (r *GormSessionRepository) FindByTokenHash(
	ctx context.Context,
	tokenHash string,
) (*SessionLookup, error) {
	var session Session

	err := r.db.WithContext(ctx).
		Where("token_hash = ? OR previous_token_hash = ?", tokenHash, tokenHash).
		First(&session).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying session by token hash: %w", err)
	}

	if session.TokenHash == tokenHash {
		return &SessionLookup{Session: &session}, nil
	}

	if session.PreviousTokenHash != nil && *session.PreviousTokenHash == tokenHash {
		return &SessionLookup{
			Session:         &session,
			MatchedPrevious: true,
		}, nil
	}

	return nil, ErrSessionNotFound
}

func (r *GormSessionRepository) Touch(
	ctx context.Context,
	sessionID uint,
	lastActivityAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ?", sessionID).
		Update("last_activity_at", lastActivityAt)
	if result.Error != nil {
		return fmt.Errorf("touching session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrSessionNotFound
	}

	return nil
}

func (r *GormSessionRepository) Update(
	ctx context.Context,
	session *Session,
) error {
	if err := r.db.WithContext(ctx).Save(session).Error; err != nil {
		return fmt.Errorf("updating session: %w", err)
	}

	return nil
}

func (r *GormSessionRepository) Revoke(
	ctx context.Context,
	sessionID uint,
	revokedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ?", sessionID).
		Update("revoked_at", revokedAt)
	if result.Error != nil {
		return fmt.Errorf("revoking session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrSessionNotFound
	}

	return nil
}
