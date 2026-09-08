package storage

import "errors"

var (
	// ErrNotFound is returned when a disk ID does not match any disk.
	ErrNotFound = errors.New("disk not found")

	// ErrAlreadyAttached is returned attaching a disk that isn't
	// currently detached.
	ErrAlreadyAttached = errors.New("disk is already attached")

	// ErrNotAttached is returned detaching/deleting-while-attached a disk
	// that isn't currently attached to anything.
	ErrNotAttached = errors.New("disk is not attached")

	// ErrStillAttached is returned deleting a disk that's still attached
	// — must be detached first.
	ErrStillAttached = errors.New("disk is still attached")

	// ErrRootDiskNotDetachable is returned trying to detach a ROOT disk
	// — real-infra semantics, same as a cloud root volume.
	ErrRootDiskNotDetachable = errors.New("root disks cannot be detached")

	// ErrWrongNode is returned attaching a disk to an instance scheduled
	// on a different node than the one the disk's file lives on — a
	// local file can't jump hosts.
	ErrWrongNode = errors.New("disk and instance are on different nodes")

	// ErrShrinkNotAllowed is returned resizing a disk to a smaller size.
	ErrShrinkNotAllowed = errors.New("disks can only grow, not shrink")

	// errNoRows is an internal repository-layer sentinel translated by
	// the service into ErrNotFound as appropriate.
	errNoRows = errors.New("no rows")
)
