package node

import "errors"

var (
	// ErrHostnameTaken is returned when registering a node whose hostname
	// already exists.
	ErrHostnameTaken = errors.New("hostname already registered")

	// ErrNotFound is returned when a node ID does not match any node.
	ErrNotFound = errors.New("node not found")

	// ErrInvalidNodeToken is returned when a node's bearer token is
	// missing, unknown, or does not match the node ID in the request.
	ErrInvalidNodeToken = errors.New("invalid node token")

	// errNoRows is an internal repository-layer sentinel translated by the
	// service into ErrNotFound / ErrInvalidNodeToken as appropriate.
	errNoRows = errors.New("no rows")

	// ErrInsufficientCapacity is returned when a node does not have enough
	// available CPU/memory/disk to satisfy a reservation request.
	ErrInsufficientCapacity = errors.New("insufficient node capacity")

	// ErrReservationConflict is returned when a reservation could not be
	// applied after repeated optimistic-concurrency retries (spec section
	// 17: version kept changing under concurrent schedulers).
	ErrReservationConflict = errors.New("reservation conflict: node changed concurrently, retry scheduling")
)
