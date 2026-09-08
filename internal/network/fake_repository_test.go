package network

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type fakeIPRecord struct {
	subnetID   string
	status     IPStatus
	instanceID string
}

type fakeRepository struct {
	mu           sync.Mutex
	nextNetID    int
	nextSubnetID int
	networks     map[string]Network
	subnets      map[string]Subnet
	ips          map[string]*fakeIPRecord // by ip address string
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		networks: make(map[string]Network),
		subnets:  make(map[string]Subnet),
		ips:      make(map[string]*fakeIPRecord),
	}
}

func (f *fakeRepository) CreateNetworkWithSubnet(ctx context.Context, name, cidr, gateway string, usableIPs []string) (Network, Subnet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.networks {
		if existing.Name == name {
			return Network{}, Subnet{}, ErrNameTaken
		}
	}

	f.nextNetID++
	n := Network{ID: fmt.Sprintf("network-%d", f.nextNetID), Name: name, CreatedAt: time.Now()}
	f.networks[n.ID] = n

	f.nextSubnetID++
	sn := Subnet{ID: fmt.Sprintf("subnet-%d", f.nextSubnetID), NetworkID: n.ID, CIDR: cidr, Gateway: gateway, CreatedAt: time.Now()}
	f.subnets[sn.ID] = sn

	for _, ip := range usableIPs {
		f.ips[ip] = &fakeIPRecord{subnetID: sn.ID, status: IPStatusAvailable}
	}

	return n, sn, nil
}

func (f *fakeRepository) List(ctx context.Context) ([]Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	networks := make([]Network, 0, len(f.networks))
	for _, n := range f.networks {
		networks = append(networks, n)
	}
	sort.Slice(networks, func(i, j int) bool { return networks[i].Name < networks[j].Name })
	return networks, nil
}

func (f *fakeRepository) Get(ctx context.Context, id string) (Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n, ok := f.networks[id]
	if !ok {
		return Network{}, errNoRows
	}
	return n, nil
}

func (f *fakeRepository) SubnetsByNetwork(ctx context.Context, networkID string) ([]Subnet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var subnets []Subnet
	for _, sn := range f.subnets {
		if sn.NetworkID == networkID {
			subnets = append(subnets, sn)
		}
	}
	sort.Slice(subnets, func(i, j int) bool { return subnets[i].CreatedAt.Before(subnets[j].CreatedAt) })
	return subnets, nil
}

func (f *fakeRepository) AllocateForInstance(ctx context.Context, instanceID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for ip, rec := range f.ips {
		if rec.status == IPStatusAllocated && rec.instanceID == instanceID {
			return ip, nil
		}
	}

	ips := make([]string, 0, len(f.ips))
	for ip := range f.ips {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	for _, ip := range ips {
		rec := f.ips[ip]
		if rec.status == IPStatusAvailable {
			rec.status = IPStatusAllocated
			rec.instanceID = instanceID
			return ip, nil
		}
	}
	return "", ErrNoCapacity
}

func (f *fakeRepository) ReleaseForInstance(ctx context.Context, instanceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, rec := range f.ips {
		if rec.status == IPStatusAllocated && rec.instanceID == instanceID {
			rec.status = IPStatusAvailable
			rec.instanceID = ""
		}
	}
	return nil
}

func (f *fakeRepository) ReserveIP(ctx context.Context, subnetID, ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	rec, ok := f.ips[ip]
	if !ok || rec.subnetID != subnetID || rec.status != IPStatusAvailable {
		return ErrIPNotAvailable
	}
	rec.status = IPStatusReserved
	return nil
}
