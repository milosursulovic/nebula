package network

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func newTestService() Service {
	return NewService(newFakeRepository())
}

func TestCreateNetworkAndGet(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	n, sn, err := svc.CreateNetwork(ctx, "production", "10.20.0.0/24", "10.20.0.1")
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if n.Name != "production" || sn.CIDR != "10.20.0.0/24" || sn.Gateway != "10.20.0.1" {
		t.Errorf("unexpected network/subnet: %+v %+v", n, sn)
	}

	got, err := svc.Get(ctx, n.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("Get returned %+v, want id %q", got, n.ID)
	}

	subnets, err := svc.SubnetsByNetwork(ctx, n.ID)
	if err != nil {
		t.Fatalf("SubnetsByNetwork: %v", err)
	}
	if len(subnets) != 1 || subnets[0].ID != sn.ID {
		t.Errorf("unexpected subnets: %+v", subnets)
	}
}

func TestCreateNetworkNameTaken(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	svc.CreateNetwork(ctx, "production", "10.20.0.0/24", "10.20.0.1")

	_, _, err := svc.CreateNetwork(ctx, "production", "10.30.0.0/24", "10.30.0.1")
	if !errors.Is(err, ErrNameTaken) {
		t.Errorf("err = %v, want ErrNameTaken", err)
	}
}

func TestCreateNetworkInvalidCIDR(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	cases := []struct {
		name, cidr, gateway string
	}{
		{"not-network-addr", "10.20.0.5/24", "10.20.0.1"},
		{"malformed-cidr", "not-a-cidr", "10.20.0.1"},
		{"gateway-outside-subnet", "10.20.0.0/24", "10.30.0.1"},
		{"gateway-not-an-ip", "10.20.0.0/24", "nope"},
		{"slash-31-no-hosts", "10.20.0.0/31", "10.20.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.CreateNetwork(ctx, tc.name, tc.cidr, tc.gateway)
			if !errors.Is(err, ErrInvalidCIDR) {
				t.Errorf("err = %v, want ErrInvalidCIDR", err)
			}
		})
	}
}

func TestCreateNetworkSubnetTooLarge(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	_, _, err := svc.CreateNetwork(ctx, "huge", "10.0.0.0/8", "10.0.0.1")
	if !errors.Is(err, ErrSubnetTooLarge) {
		t.Errorf("err = %v, want ErrSubnetTooLarge", err)
	}
}

func TestGetNotFound(t *testing.T) {
	svc := newTestService()

	if _, err := svc.Get(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAllocateForInstanceExcludesReservedAddresses(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	// A /30 (10.20.0.0-3) has 2 usable host addresses (.1, .2); with .1
	// as gateway, only .2 is ever allocatable.
	_, _, err := svc.CreateNetwork(ctx, "tiny", "10.20.0.0/30", "10.20.0.1")
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}

	ip, err := svc.AllocateForInstance(ctx, "inst-1")
	if err != nil {
		t.Fatalf("AllocateForInstance: %v", err)
	}
	if ip != "10.20.0.2" {
		t.Errorf("ip = %q, want 10.20.0.2", ip)
	}

	if _, err := svc.AllocateForInstance(ctx, "inst-2"); !errors.Is(err, ErrNoCapacity) {
		t.Errorf("second allocate err = %v, want ErrNoCapacity", err)
	}
}

func TestAllocateForInstanceIsIdempotent(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	svc.CreateNetwork(ctx, "production", "10.20.0.0/24", "10.20.0.1")

	first, err := svc.AllocateForInstance(ctx, "inst-1")
	if err != nil {
		t.Fatalf("first AllocateForInstance: %v", err)
	}
	second, err := svc.AllocateForInstance(ctx, "inst-1")
	if err != nil {
		t.Fatalf("second AllocateForInstance: %v", err)
	}
	if first != second {
		t.Errorf("retried allocate returned a different ip: %q vs %q", first, second)
	}
}

func TestReleaseForInstanceThenReallocate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	svc.CreateNetwork(ctx, "tiny", "10.20.0.0/30", "10.20.0.1")

	ip1, err := svc.AllocateForInstance(ctx, "inst-1")
	if err != nil {
		t.Fatalf("AllocateForInstance: %v", err)
	}

	if err := svc.ReleaseForInstance(ctx, "inst-1"); err != nil {
		t.Fatalf("ReleaseForInstance: %v", err)
	}

	ip2, err := svc.AllocateForInstance(ctx, "inst-2")
	if err != nil {
		t.Fatalf("AllocateForInstance after release: %v", err)
	}
	if ip1 != ip2 {
		t.Errorf("expected the released ip to be reallocated: got %q want %q", ip2, ip1)
	}
}

func TestReleaseForInstanceUnknownIsNoop(t *testing.T) {
	svc := newTestService()

	if err := svc.ReleaseForInstance(context.Background(), "never-allocated"); err != nil {
		t.Fatalf("ReleaseForInstance on unknown instance: %v", err)
	}
}

func TestReserveIP(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, sn, _ := svc.CreateNetwork(ctx, "production", "10.20.0.0/24", "10.20.0.1")

	if err := svc.ReserveIP(ctx, sn.ID, "10.20.0.5"); err != nil {
		t.Fatalf("ReserveIP: %v", err)
	}

	// A reserved address must not come back from AllocateForInstance's
	// general pool.
	for i := 0; i < 253; i++ {
		ip, err := svc.AllocateForInstance(ctx, fmt.Sprintf("inst-%d", i))
		if err != nil {
			break
		}
		if ip == "10.20.0.5" {
			t.Fatalf("reserved ip 10.20.0.5 was handed out by AllocateForInstance")
		}
	}
}

func TestReserveIPNotAvailable(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, sn, _ := svc.CreateNetwork(ctx, "production", "10.20.0.0/24", "10.20.0.1")

	if err := svc.ReserveIP(ctx, sn.ID, "10.20.0.5"); err != nil {
		t.Fatalf("ReserveIP: %v", err)
	}
	if err := svc.ReserveIP(ctx, sn.ID, "10.20.0.5"); !errors.Is(err, ErrIPNotAvailable) {
		t.Errorf("err = %v, want ErrIPNotAvailable", err)
	}
}
