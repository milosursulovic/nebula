package agent

import "errors"

// ErrVMNotFound is returned when an instance ID doesn't match any VM this
// agent knows about.
var ErrVMNotFound = errors.New("vm not found")

// ErrDiskNotFound is returned when a disk ID doesn't match any disk file
// this agent knows about.
var ErrDiskNotFound = errors.New("disk not found")
