package storage

import "time"

// Type is a disk's role (spec section 34).
type Type string

const (
	TypeRoot   Type = "ROOT"
	TypeData   Type = "DATA"
	TypeBackup Type = "BACKUP"
)

// Disk is a virtual disk — spec section 34: "initially use files or
// sparse files." The real sparse file lives on NodeID's filesystem
// (internal/agent's DiskStore); this row is the control-plane's record of
// it. Attachment state is just InstanceID being nil or not — no separate
// status column needed.
type Disk struct {
	ID         string
	TenantID   string
	InstanceID *string // nil when detached
	NodeID     string  // fixed at creation — the file lives on this node
	Type       Type
	SizeGB     int
	FilePath   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
