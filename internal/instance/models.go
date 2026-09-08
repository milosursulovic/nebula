package instance

import "time"

// Status is an instance's lifecycle state (spec section 13).
type Status string

const (
	StatusPending      Status = "PENDING"
	StatusProvisioning Status = "PROVISIONING"
	StatusRunning      Status = "RUNNING"
	StatusStopping     Status = "STOPPING"
	StatusStopped      Status = "STOPPED"
	StatusError        Status = "ERROR"
	StatusDeleting     Status = "DELETING"
	StatusDeleted      Status = "DELETED"
)

// allowedTransitions is the explicit state transition graph (spec section
// 13: "Do not allow arbitrary transitions"). ERROR->PROVISIONING (Phase 8)
// is what lets a job retry re-run the provisioning saga on an instance that
// failed a previous attempt — the saga always transitions into PROVISIONING
// as its first step, before scheduling, so a retry needs a way back in.
var allowedTransitions = map[Status][]Status{
	StatusPending:      {StatusProvisioning, StatusDeleting},
	StatusProvisioning: {StatusRunning, StatusError},
	StatusRunning:      {StatusStopping, StatusDeleting},
	StatusStopping:     {StatusStopped, StatusError},
	StatusStopped:      {StatusRunning, StatusDeleting},
	StatusError:        {StatusDeleting, StatusProvisioning},
	StatusDeleting:     {StatusDeleted},
	StatusDeleted:      {},
}

// CanTransition reports whether moving from one status to another is a
// legal state transition.
func CanTransition(from, to Status) bool {
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Instance is a virtual machine record (spec section 4). No hypervisor
// backs it yet — that arrives once the agent/KVM phases exist.
type Instance struct {
	ID        string
	TenantID  string
	Name      string
	Status    Status
	CPU       int
	MemoryMB  int
	DiskGB    int
	Image     string
	NodeID    *string // assigned by the scheduler; nil until then
	IPAddress *string // assigned by the provisioning saga's network step; nil until then
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateInput is what a caller supplies to create an instance (spec
// section 14's example request).
type CreateInput struct {
	Name     string
	CPU      int
	MemoryMB int
	DiskGB   int
	Image    string
}
