package agent

import "fmt"

// NewHypervisor builds the Hypervisor selected by cfg.HypervisorBackend
// ("mock", the default — works in every build; "libvirt" — only
// resolvable when built with -tags libvirt, see hypervisor_default.go /
// hypervisor_libvirt.go).
func NewHypervisor(cfg Config) (Hypervisor, error) {
	switch cfg.HypervisorBackend {
	case "", "mock":
		return NewMockHypervisor(NewStore()), nil
	case "libvirt":
		return newLibvirtHypervisor(cfg.LibvirtURI)
	default:
		return nil, fmt.Errorf("unknown NEBULA_AGENT_HYPERVISOR %q (want \"mock\" or \"libvirt\")", cfg.HypervisorBackend)
	}
}
