package auth

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type GormSessionRepository struct {
	db *gorm.DB
}

var _ SessionRepository = (*GormSessionRepository)(nil)
var _ SessionRevoker = (*GormSessionRepository)(nil)
var _ SessionRotationRequester = (*GormSessionRepository)(nil)

func NewGormSessionRepository(db *gorm.DB) *GormSessionRepository {
	return &GormSessionRepository{db: db}
}

func (r *GormSessionRepository) Create(ctx context.Context, session *Session) error {
	if err := r.db.WithContext(ctx).Create(session).Error; err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	return nil
}

func (r *GormSessionRepository) FindByID(ctx context.Context, sessionID uint) (*Session, error) {
	var session Session
	result := r.db.WithContext(ctx).Where("id = ?", sessionID).Limit(1).Find(&session)
	if result.Error != nil {
		return nil, fmt.Errorf("querying session by id: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrSessionNotFound
	}
	return &session, nil
}

func (r *GormSessionRepository) FindByTokenHash(
	ctx context.Context,
	tokenHash string,
) (*SessionLookup, error) {
	var session Session
	result := r.db.WithContext(ctx).
		Where("token_hash = ? OR previous_token_hash = ?", tokenHash, tokenHash).
		Limit(1).
		Find(&session)
	if result.Error != nil {
		return nil, fmt.Errorf("querying session by token hash: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, ErrSessionNotFound
	}
	if session.TokenHash == tokenHash {
		return &SessionLookup{Session: &session}, nil
	}
	if session.PreviousTokenHash != nil && *session.PreviousTokenHash == tokenHash {
		return &SessionLookup{Session: &session, MatchedPrevious: true}, nil
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

func (r *GormSessionRepository) Update(ctx context.Context, session *Session) error {
	result := r.db.WithContext(ctx).
		Model(&Session{}).
		Where("id = ? AND revoked_at IS NULL", session.ID).
		Updates(map[string]any{
			"token_hash":                session.TokenHash,
			"previous_token_hash":       session.PreviousTokenHash,
			"previous_token_expires_at": session.PreviousTokenExpiresAt,
			"last_activity_at":          session.LastActivityAt,
			"last_rotated_at":           session.LastRotatedAt,
			"rotation_required":         session.RotationRequired,
		})
	if result.Error != nil {
		return fmt.Errorf("updating session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		current, err := r.FindByID(ctx, session.ID)
		if err != nil {
			return err
		}
		if current.RevokedAt != nil {
			return ErrSessionRevoked
		}
		return ErrSessionNotFound
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

func (r *GormSessionRepository) RequireRotationByUser(ctx context.Context, userID uint) error {
	if err := r.db.WithContext(ctx).
		Model(&Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("rotation_required", true).Error; err != nil {
		return fmt.Errorf("requiring session rotation for user: %w", err)
	}
	return nil
}

func (r *GormSessionRepository) RevokeAllByUser(
	ctx context.Context,
	userID uint,
	revokedAt time.Time,
) error {
	if err := r.db.WithContext(ctx).
		Model(&Session{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", revokedAt).Error; err != nil {
		return fmt.Errorf("revoking user sessions: %w", err)
	}
	return nil
}
