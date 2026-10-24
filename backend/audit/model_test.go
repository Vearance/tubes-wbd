package audit

import (
	"context"
	"strings"
	"testing"
)

func TestAppendRejectsInvalidEventsBeforeDatabaseWrite(t *testing.T) {
	repository := NewGormRepository(nil)
	for _, event := range []Event{
		{Action: "arbitrary.action", Outcome: OutcomeSuccess},
		{Action: ActionUserCreated, Outcome: "unknown"},
		{Action: ActionUserCreated, Outcome: OutcomeSuccess, TargetType: TargetUser},
	} {
		if err := repository.Append(context.Background(), event); err == nil {
			t.Fatalf("accepted invalid event: %+v", event)
		}
	}
}

func TestSafeActorRemovesControlsAndBoundsLength(t *testing.T) {
	got := SafeActor("  alice\n\r\tbob  ")
	if got != "alicebob" {
		t.Fatalf("SafeActor = %q", got)
	}
	long := SafeActor(strings.Repeat("界", 100))
	if len(long) > 255 || !strings.HasSuffix(long, "界") {
		t.Fatalf("invalid bounded UTF-8 actor: %q", long)
	}
}

func TestSourceIPUsesDirectPeer(t *testing.T) {
	if got := SourceIP("[2001:db8::1]:4321"); got != "2001:db8::1" {
		t.Fatalf("SourceIP = %q", got)
	}
	if got := SourceIP("not-a-peer"); got != "" {
		t.Fatalf("unexpected SourceIP = %q", got)
	}
}
