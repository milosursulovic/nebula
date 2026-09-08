package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiskStore manages real sparse disk files on this node's local
// filesystem (spec section 34: "initially use files or sparse files").
// A Truncate to the target size without ever writing bytes creates a
// sparse file — it reports its full logical size but consumes no real
// disk space until something is actually written into it.
type DiskStore struct {
	root string
}

// NewDiskStore ensures root exists and returns a DiskStore rooted there.
func NewDiskStore(root string) (*DiskStore, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create disk root %s: %w", root, err)
	}
	return &DiskStore{root: root}, nil
}

func (d *DiskStore) path(diskID string) string {
	return filepath.Join(d.root, diskID+".img")
}

// Create makes a new sparse file sized sizeGB and returns its path.
func (d *DiskStore) Create(diskID string, sizeGB int) (string, error) {
	path := d.path(diskID)

	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create disk file: %w", err)
	}
	defer f.Close()

	if err := f.Truncate(int64(sizeGB) << 30); err != nil {
		return "", fmt.Errorf("truncate disk file to %d GB: %w", sizeGB, err)
	}
	return path, nil
}

// Delete removes a disk's file. Deleting an unknown diskID is a no-op
// success (compensation may call this after a step that never created
// anything — same idempotent-delete shape as Store.Delete).
func (d *DiskStore) Delete(diskID string) error {
	if err := os.Remove(d.path(diskID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete disk file: %w", err)
	}
	return nil
}

// Resize grows a disk's file to newSizeGB. Shrinking is rejected — a
// disk can lose data if truncated smaller, same as real cloud storage
// APIs only ever allow growing a live volume.
func (d *DiskStore) Resize(diskID string, newSizeGB int) error {
	path := d.path(diskID)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrDiskNotFound
		}
		return fmt.Errorf("stat disk file: %w", err)
	}

	newSize := int64(newSizeGB) << 30
	if newSize < info.Size() {
		return fmt.Errorf("cannot shrink disk from %d bytes to %d bytes", info.Size(), newSize)
	}

	f, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open disk file: %w", err)
	}
	defer f.Close()

	if err := f.Truncate(newSize); err != nil {
		return fmt.Errorf("truncate disk file to %d GB: %w", newSizeGB, err)
	}
	return nil
}
