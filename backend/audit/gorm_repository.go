package audit

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"gorm.io/gorm"
)

type GormRepository struct{ db *gorm.DB }

var _ Repository = (*GormRepository)(nil)

func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("audit database is nil")
	}
	return db.AutoMigrate(&Event{})
}

func NewGormRepository(db *gorm.DB) *GormRepository { return &GormRepository{db: db} }

func (r *GormRepository) Append(ctx context.Context, event Event) error {
	if !ValidAction(event.Action) || !ValidOutcome(event.Outcome) {
		return errors.New("invalid audit event")
	}
	if (event.TargetType == "") != (event.TargetID == nil) || (event.TargetType != "" && !ValidTargetType(event.TargetType)) {
		return errors.New("invalid audit target")
	}
	event.ID = 0
	event.OccurredAt = time.Now().UTC()
	event.ActorUsername = SafeActor(event.ActorUsername)
	if event.SourceIP != "" {
		address, err := netip.ParseAddr(event.SourceIP)
		if err != nil {
			return errors.New("invalid audit source IP")
		}
		event.SourceIP = address.String()
	}
	if err := r.db.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("appending audit event: %w", err)
	}
	return nil
}

func (r *GormRepository) List(ctx context.Context, filter Filter) (Page, error) {
	if filter.Limit == 0 {
		filter.Limit = DefaultLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxLimit {
		return Page{}, errors.New("invalid audit page limit")
	}
	if filter.Actor != SafeActor(filter.Actor) || (filter.Action != "" && !ValidAction(filter.Action)) || (filter.Outcome != "" && !ValidOutcome(filter.Outcome)) {
		return Page{}, errors.New("invalid audit filter")
	}
	query := r.db.WithContext(ctx).Model(&Event{})
	if filter.Actor != "" {
		query = query.Where("actor_username = ?", filter.Actor)
	}
	if filter.Action != "" {
		query = query.Where("action = ?", filter.Action)
	}
	if filter.Outcome != "" {
		query = query.Where("outcome = ?", filter.Outcome)
	}
	if filter.BeforeID > 0 {
		query = query.Where("id < ?", filter.BeforeID)
	}
	var events []Event
	if err := query.Order("id DESC").Limit(filter.Limit + 1).Find(&events).Error; err != nil {
		return Page{}, fmt.Errorf("listing audit events: %w", err)
	}
	page := Page{Events: events}
	if len(events) > filter.Limit {
		page.Events = events[:filter.Limit]
		lastID := page.Events[len(page.Events)-1].ID
		page.NextBeforeID = &lastID
	}
	return page, nil
}
