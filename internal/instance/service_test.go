package instance

import (
	"context"
	"errors"
	"testing"
)

func newTestService() Service {
	return NewService(newFakeRepository())
}

func TestCreateInstance(t *testing.T) {
	svc := newTestService()

	created, err := svc.Create(context.Background(), "tenant-1", CreateInput{
		Name: "database-01", CPU: 4, MemoryMB: 8192, DiskGB: 100, Image: "ubuntu-26.04",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != StatusPending {
		t.Errorf("Status = %q, want %q", created.Status, StatusPending)
	}
	if created.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", created.TenantID, "tenant-1")
	}
}

func TestCreateDuplicateNameSameTenant(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	in := CreateInput{Name: "database-01", CPU: 4, MemoryMB: 8192, DiskGB: 100, Image: "ubuntu-26.04"}

	if _, err := svc.Create(ctx, "tenant-1", in); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := svc.Create(ctx, "tenant-1", in)
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("Create duplicate = %v, want ErrNameTaken", err)
	}
}

func TestCreateSameNameDifferentTenantsAllowed(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	in := CreateInput{Name: "database-01", CPU: 4, MemoryMB: 8192, DiskGB: 100, Image: "ubuntu-26.04"}

	if _, err := svc.Create(ctx, "tenant-1", in); err != nil {
		t.Fatalf("Create tenant-1: %v", err)
	}
	if _, err := svc.Create(ctx, "tenant-2", in); err != nil {
		t.Fatalf("Create tenant-2 with same name: %v", err)
	}
}

func TestListScopedToTenant(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	if _, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, "tenant-2", CreateInput{Name: "b", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := svc.List(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Name != "a" {
		t.Errorf("List(tenant-1) = %+v, want just instance 'a'", list)
	}
}

func TestGetFromWrongTenantNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Get(ctx, "tenant-2", created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get from wrong tenant = %v, want ErrNotFound", err)
	}
}

func TestDeleteHappyPath(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	deleted, err := svc.Delete(ctx, "tenant-1", created.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted.Status != StatusDeleted {
		t.Errorf("Status = %q, want %q", deleted.Status, StatusDeleted)
	}

	got, err := svc.Get(ctx, "tenant-1", created.ID)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got.Status != StatusDeleted {
		t.Errorf("Get after delete Status = %q, want %q", got.Status, StatusDeleted)
	}
}

func TestDeleteTwiceIsInvalidTransition(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Delete(ctx, "tenant-1", created.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}

	_, err = svc.Delete(ctx, "tenant-1", created.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second Delete = %v, want ErrInvalidTransition", err)
	}
}

func TestDeleteFromWrongTenantNotFound(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	created, err := svc.Create(ctx, "tenant-1", CreateInput{Name: "a", CPU: 1, MemoryMB: 1, DiskGB: 1, Image: "img"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Delete(ctx, "tenant-2", created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete from wrong tenant = %v, want ErrNotFound", err)
	}
}
