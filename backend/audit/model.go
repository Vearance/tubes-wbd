package audit

import (
	"net"
	"net/netip"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ActionLogin             = "auth.login"
	ActionLogout            = "auth.logout"
	ActionAuditRead         = "admin.audit.read"
	ActionUserCreated       = "admin.user.created"
	ActionUserUpdated       = "admin.user.updated"
	ActionUserDisabled      = "admin.user.disabled"
	ActionGroupCreated      = "admin.group.created"
	ActionGroupUpdated      = "admin.group.updated"
	ActionMemberAdded       = "admin.group.member.added"
	ActionMemberRemoved     = "admin.group.member.removed"
	ActionAccessGranted     = "admin.group.access.granted"
	ActionAccessChanged     = "admin.group.access.changed"
	ActionAccessRevoked     = "admin.group.access.revoked"
	ActionConnectionCreated = "admin.database_connection.created"
	ActionConnectionUpdated = "admin.database_connection.updated"
	ActionConnectionTested  = "admin.database_connection.tested"
	TargetUser              = "user"
	TargetGroup             = "group"
	TargetConnection        = "database_connection"
	OutcomeSuccess          = "success"
	OutcomeFailure          = "failure"
	OutcomeDenied           = "denied"
	DefaultLimit            = 50
	MaxLimit                = 100
)

// Event contains only allowlisted audit fields. No request body, credential, or token is stored.
type Event struct {
	ID            uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	OccurredAt    time.Time `gorm:"not null;index" json:"occurred_at"`
	ActorUserID   *uint     `gorm:"index" json:"actor_user_id,omitempty"`
	ActorUsername string    `gorm:"size:255;index" json:"actor_username"`
	Action        string    `gorm:"size:64;not null;index" json:"action"`
	Outcome       string    `gorm:"size:16;not null;index" json:"outcome"`
	TargetType    string    `gorm:"size:32;index:idx_audit_target" json:"target_type,omitempty"`
	TargetID      *uint     `gorm:"index:idx_audit_target" json:"target_id,omitempty"`
	SourceIP      string    `gorm:"size:45" json:"source_ip,omitempty"`
}

func (Event) TableName() string { return "audit_events" }

type Filter struct {
	Actor    string
	Action   string
	Outcome  string
	BeforeID uint64
	Limit    int
}

type Page struct {
	Events       []Event
	NextBeforeID *uint64
}

// SafeActor bounds and removes control characters from a supplied username.
func SafeActor(value string) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, ""))
	var builder strings.Builder
	for _, char := range value {
		if unicode.IsControl(char) {
			continue
		}
		if builder.Len()+utf8.RuneLen(char) > 255 {
			break
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

func ValidAction(value string) bool {
	switch value {
	case ActionLogin, ActionLogout, ActionAuditRead,
		ActionUserCreated, ActionUserUpdated, ActionUserDisabled,
		ActionGroupCreated, ActionGroupUpdated, ActionMemberAdded, ActionMemberRemoved,
		ActionAccessGranted, ActionAccessChanged, ActionAccessRevoked,
		ActionConnectionCreated, ActionConnectionUpdated, ActionConnectionTested:
		return true
	}
	return false
}

func ValidTargetType(value string) bool {
	switch value {
	case TargetUser, TargetGroup, TargetConnection:
		return true
	}
	return false
}

func ValidOutcome(value string) bool {
	switch value {
	case OutcomeSuccess, OutcomeFailure, OutcomeDenied:
		return true
	}
	return false
}

// SourceIP uses the direct peer address. Forwarded headers are deliberately ignored.
func SourceIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return ""
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	return address.String()
}
