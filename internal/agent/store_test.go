package agent

import (
	"errors"
	"testing"
)

func TestStoreCreateAndGet(t *testing.T) {
	s := NewStore()
	created := s.Create("inst-1", 2, 4096, 50, "ubuntu-26.04")

	if created.Status != VMStatusStopped {
		t.Errorf("created status = %q, want STOPPED", created.Status)
	}

	got, err := s.Get("inst-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CPU != 2 || got.MemoryMB != 4096 || got.DiskGB != 50 || got.Image != "ubuntu-26.04" {
		t.Errorf("unexpected VM: %+v", got)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	s := NewStore()
	if _, err := s.Get("missing"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("err = %v, want ErrVMNotFound", err)
	}
}

func TestStoreStart(t *testing.T) {
	s := NewStore()
	s.Create("inst-1", 1, 1024, 10, "img")

	started, err := s.Start("inst-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if started.Status != VMStatusRunning {
		t.Errorf("status = %q, want RUNNING", started.Status)
	}
}

func TestStoreStartNotFound(t *testing.T) {
	s := NewStore()
	if _, err := s.Start("missing"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("err = %v, want ErrVMNotFound", err)
	}
}

func TestStoreDelete(t *testing.T) {
	s := NewStore()
	s.Create("inst-1", 1, 1024, 10, "img")
	s.Delete("inst-1")

	if _, err := s.Get("inst-1"); !errors.Is(err, ErrVMNotFound) {
		t.Errorf("err = %v, want ErrVMNotFound after delete", err)
	}
}

func TestStoreDeleteUnknownIsNoop(t *testing.T) {
	s := NewStore()
	s.Delete("never-existed") // must not panic
}

func TestStoreCreateOverwritesExisting(t *testing.T) {
	s := NewStore()
	s.Create("inst-1", 1, 1024, 10, "img-a")
	s.Start("inst-1")

	recreated := s.Create("inst-1", 2, 2048, 20, "img-b")
	if recreated.Status != VMStatusStopped {
		t.Errorf("recreated status = %q, want STOPPED (fresh record)", recreated.Status)
	}
	if recreated.Image != "img-b" {
		t.Errorf("recreated image = %q, want img-b", recreated.Image)
	}
}

func TestStoreCount(t *testing.T) {
	s := NewStore()
	if s.Count() != 0 {
		t.Errorf("initial Count = %d, want 0", s.Count())
	}
	s.Create("inst-1", 1, 1024, 10, "img")
	s.Create("inst-2", 1, 1024, 10, "img")
	if s.Count() != 2 {
		t.Errorf("Count = %d, want 2", s.Count())
	}
	s.Delete("inst-1")
	if s.Count() != 1 {
		t.Errorf("Count after delete = %d, want 1", s.Count())
	}
}
