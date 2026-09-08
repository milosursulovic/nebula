package agent

import (
	"errors"
	"os"
	"testing"
)

func TestDiskStoreCreateAndResize(t *testing.T) {
	store, err := NewDiskStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewDiskStore: %v", err)
	}

	path, err := store.Create("disk-1", 1)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat created file: %v", err)
	}
	if info.Size() != 1<<30 {
		t.Errorf("size = %d, want %d (1 GB)", info.Size(), int64(1)<<30)
	}

	if err := store.Resize("disk-1", 2); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat after resize: %v", err)
	}
	if info.Size() != 2<<30 {
		t.Errorf("size after resize = %d, want %d (2 GB)", info.Size(), int64(2)<<30)
	}
}

func TestDiskStoreResizeRejectsShrink(t *testing.T) {
	store, _ := NewDiskStore(t.TempDir())
	store.Create("disk-1", 5)

	if err := store.Resize("disk-1", 2); err == nil {
		t.Error("expected an error shrinking a disk, got nil")
	}
}

func TestDiskStoreResizeNotFound(t *testing.T) {
	store, _ := NewDiskStore(t.TempDir())

	if err := store.Resize("missing", 5); !errors.Is(err, ErrDiskNotFound) {
		t.Errorf("err = %v, want ErrDiskNotFound", err)
	}
}

func TestDiskStoreDelete(t *testing.T) {
	store, _ := NewDiskStore(t.TempDir())
	path, _ := store.Create("disk-1", 1)

	if err := store.Delete("disk-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be gone after Delete")
	}
}

func TestDiskStoreDeleteUnknownIsNoop(t *testing.T) {
	store, _ := NewDiskStore(t.TempDir())

	if err := store.Delete("never-existed"); err != nil {
		t.Fatalf("Delete on unknown disk: %v", err)
	}
}

func TestDiskStoreCreateOverwritesExisting(t *testing.T) {
	store, _ := NewDiskStore(t.TempDir())
	path, _ := store.Create("disk-1", 5)

	newPath, err := store.Create("disk-1", 1)
	if err != nil {
		t.Fatalf("re-Create: %v", err)
	}
	if newPath != path {
		t.Errorf("path changed on re-create: %q vs %q", newPath, path)
	}
	info, _ := os.Stat(path)
	if info.Size() != 1<<30 {
		t.Errorf("size after re-create = %d, want %d (1 GB)", info.Size(), int64(1)<<30)
	}
}
