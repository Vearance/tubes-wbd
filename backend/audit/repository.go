package audit

import "context"

type Repository interface {
	Append(ctx context.Context, event Event) error
	List(ctx context.Context, filter Filter) (Page, error)
}
