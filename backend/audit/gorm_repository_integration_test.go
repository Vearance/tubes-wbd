//go:build integration

package audit

import (
	"context"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGormRepositoryAppendFilterAndPaginate(t *testing.T) {
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("audit_test"),
		tcpostgres.WithUsername("audit_test"),
		tcpostgres.WithPassword("audit_test"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormRepository(db)
	for _, event := range []Event{
		{ActorUsername: "alice", Action: ActionLogin, Outcome: OutcomeSuccess},
		{ActorUsername: "bob", Action: ActionLogin, Outcome: OutcomeFailure},
		{ActorUsername: "alice", Action: ActionAuditRead, Outcome: OutcomeSuccess},
	} {
		if err := repository.Append(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repository.List(ctx, Filter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.NextBeforeID == nil || page.Events[0].ID <= page.Events[1].ID {
		t.Fatalf("first page = %+v", page)
	}
	next, err := repository.List(ctx, Filter{Limit: 2, BeforeID: *page.NextBeforeID})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Events) != 1 || next.NextBeforeID != nil {
		t.Fatalf("next page = %+v", next)
	}
	alice, err := repository.List(ctx, Filter{Limit: 10, Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(alice.Events) != 2 {
		t.Fatalf("alice events = %+v", alice.Events)
	}
	defaultPage, err := repository.List(ctx, Filter{})
	if err != nil || len(defaultPage.Events) != 3 {
		t.Fatalf("default page = %+v, %v", defaultPage, err)
	}
}
