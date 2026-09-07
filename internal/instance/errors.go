package instance

import "errors"

var (
	// ErrNotFound is returned when an instance ID doesn't match any
	// instance within the caller's tenant.
	ErrNotFound = errors.New("instance not found")

	// ErrNameTaken is returned when creating an instance whose name is
	// already used within the same tenant.
	ErrNameTaken = errors.New("instance name already in use")

	// ErrInvalidTransition is returned when a requested state change is
	// not a legal edge in the instance state machine.
	ErrInvalidTransition = errors.New("invalid instance state transition")

	// errNoRows is an internal repository-layer sentinel translated by
	// the service into ErrNotFound / ErrInvalidTransition as appropriate.
	errNoRows = errors.New("no rows")
)
