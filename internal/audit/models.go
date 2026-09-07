package audit

import (
	"time"

	"github.com/milosursulovic/nebula/internal/outbox"
)

// Record is a row in audit_logs (spec sections 5 and 38).
type Record struct {
	ID           string
	EventID      string // outbox_events.id — unique, makes inserts idempotent
	TenantID     *string
	Action       string
	ResourceType string
	ResourceID   string
	Metadata     []byte
	CreatedAt    time.Time
}

// actionFor maps a Kafka domain event type (spec section 22's PascalCase
// names) to the SCREAMING_SNAKE audit action spec section 38 uses in its
// own examples (INSTANCE_CREATED, NODE_OFFLINE, ...).
var actionFor = map[string]string{
	outbox.EventInstanceCreated:             "INSTANCE_CREATED",
	outbox.EventInstanceProvisioningStarted: "INSTANCE_PROVISIONING_STARTED",
	outbox.EventInstanceProvisioned:         "INSTANCE_PROVISIONED",
	outbox.EventInstanceProvisioningFailed:  "INSTANCE_PROVISIONING_FAILED",
	outbox.EventInstanceDeleted:             "INSTANCE_DELETED",
	outbox.EventNodeOnline:                  "NODE_ONLINE",
	outbox.EventNodeOffline:                 "NODE_OFFLINE",
}

// ActionFor returns the audit action for a Kafka event type, and whether
// one is known. Unknown event types (e.g. a future Network/Disk event this
// consumer doesn't handle yet) are skipped rather than guessed at.
func ActionFor(eventType string) (string, bool) {
	a, ok := actionFor[eventType]
	return a, ok
}
