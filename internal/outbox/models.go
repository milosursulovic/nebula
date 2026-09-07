package outbox

import "time"

// Event types (spec section 22's list). Only the ones actually emitted
// this phase (instance + node lifecycle) have a producer; Network/Disk
// events are named here to match spec's documented list but nothing
// emits them yet — those domains don't exist until later phases.
const (
	EventInstanceCreated             = "InstanceCreated"
	EventInstanceProvisioningStarted = "InstanceProvisioningStarted"
	EventInstanceProvisioned         = "InstanceProvisioned"
	EventInstanceProvisioningFailed  = "InstanceProvisioningFailed"
	EventInstanceDeleted             = "InstanceDeleted"

	EventNodeOnline  = "NodeOnline"
	EventNodeOffline = "NodeOffline"

	EventNetworkCreated = "NetworkCreated"
	EventNetworkDeleted = "NetworkDeleted"
	EventDiskCreated    = "DiskCreated"
	EventDiskDeleted    = "DiskDeleted"
)

// AggregateType names the kind of entity an event is about.
const (
	AggregateInstance = "instance"
	AggregateNode     = "node"
)

// Event is a row in outbox_events (spec section 23's exact fields).
type Event struct {
	ID            string
	EventType     string
	AggregateType string
	AggregateID   string
	Payload       []byte
	CreatedAt     time.Time
	PublishedAt   *time.Time
}
