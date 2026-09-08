//go:build !libvirt

package agent

import "fmt"

// newLibvirtHypervisor is shadowed by hypervisor_libvirt.go when built
// with -tags libvirt (which needs CGO + libvirt dev headers, not part of
// the default build/Docker image). Without that tag, selecting the
// "libvirt" backend fails loudly at startup instead of silently falling
// back to mock.
func newLibvirtHypervisor(uri string) (Hypervisor, error) {
	return nil, fmt.Errorf("hypervisor backend %q requires building with -tags libvirt", "libvirt")
}
