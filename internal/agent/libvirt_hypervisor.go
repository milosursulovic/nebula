//go:build libvirt

package agent

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"

	libvirtgo "libvirt.org/go/libvirt"
)

// nebulaMetadataNS is the custom libvirt domain-metadata namespace used to
// stash DiskGB/Image — libvirt's own domain model has no native concept
// of either at this scope (no <devices><disk>/<interface> — see
// LibvirtHypervisor's doc comment for why).
const nebulaMetadataNS = "https://nebula.local/schema"

// LibvirtHypervisor implements Hypervisor against a real libvirt daemon
// (spec section 65: "Implement LibvirtHypervisor... start with a single
// local Linux machine"). Domains it creates are deliberately
// headless/diskless/netless — real disks and networking are Phase 13
// (Storage) and Phase 12 (Networking)'s job, not this one; this phase's
// honest scope is proving the create/delete/start/stop/status lifecycle
// against real KVM.
type LibvirtHypervisor struct {
	conn *libvirtgo.Connect
}

// newLibvirtHypervisor is only linked in when built with -tags libvirt
// (see hypervisor_default.go for the non-tagged stub).
func newLibvirtHypervisor(uri string) (Hypervisor, error) {
	conn, err := libvirtgo.NewConnect(uri)
	if err != nil {
		return nil, fmt.Errorf("connect to libvirt at %q: %w", uri, err)
	}
	return &LibvirtHypervisor{conn: conn}, nil
}

type domainXML struct {
	XMLName xml.Name     `xml:"domain"`
	Type    string       `xml:"type,attr"`
	Name    string       `xml:"name"`
	Memory  domainMemory `xml:"memory"`
	VCPU    int          `xml:"vcpu"`
	OS      domainOS     `xml:"os"`
	Meta    domainMeta   `xml:"metadata"`
}

type domainMemory struct {
	Unit  string `xml:"unit,attr"`
	Value int    `xml:",chardata"`
}

type domainOS struct {
	Type domainOSType `xml:"type"`
}

type domainOSType struct {
	Arch  string `xml:"arch,attr"`
	Value string `xml:",chardata"`
}

type domainMeta struct {
	Info nebulaInfo `xml:"nebula:info"`
}

// nebulaInfo marshals the custom domain-metadata element libvirt requires
// to be namespaced at the top level (bare "info" tag would be silently
// rejected/ignored on the way back out via GetMetadata) — both the
// element name ("nebula:info") and its xmlns declaration are written via
// the literal-colon struct-tag trick, since encoding/xml has no cleaner
// way to declare a custom prefix from tags alone.
type nebulaInfo struct {
	XMLName xml.Name `xml:"nebula:info"`
	XMLNS   string   `xml:"xmlns:nebula,attr"`
	DiskGB  int      `xml:"disk_gb"`
	Image   string   `xml:"image"`
}

// nebulaInfoIn unmarshals GetMetadata's returned fragment — deliberately
// no XMLName tag, so it matches whatever the (single, root) element
// actually is regardless of namespace/prefix, and matches its children by
// local name only (encoding/xml ignores namespace when a tag has no
// namespace component).
type nebulaInfoIn struct {
	DiskGB int    `xml:"disk_gb"`
	Image  string `xml:"image"`
}

func (h *LibvirtHypervisor) CreateVM(ctx context.Context, spec VMSpec) error {
	doc := domainXML{
		Type: "kvm",
		Name: spec.InstanceID,
		Memory: domainMemory{
			Unit:  "MiB",
			Value: spec.MemoryMB,
		},
		VCPU: spec.CPU,
		OS: domainOS{
			Type: domainOSType{Arch: "x86_64", Value: "hvm"},
		},
		Meta: domainMeta{
			Info: nebulaInfo{
				XMLNS:  nebulaMetadataNS,
				DiskGB: spec.DiskGB,
				Image:  spec.Image,
			},
		},
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode domain xml: %w", err)
	}

	dom, err := h.conn.DomainDefineXML(buf.String())
	if err != nil {
		return fmt.Errorf("define domain: %w", err)
	}
	defer dom.Free()
	return nil
}

func (h *LibvirtHypervisor) lookup(instanceID string) (*libvirtgo.Domain, error) {
	dom, err := h.conn.LookupDomainByName(instanceID)
	if err != nil {
		var lerr libvirtgo.Error
		if errors.As(err, &lerr) && lerr.Code == libvirtgo.ERR_NO_DOMAIN {
			return nil, ErrVMNotFound
		}
		return nil, fmt.Errorf("lookup domain %s: %w", instanceID, err)
	}
	return dom, nil
}

func (h *LibvirtHypervisor) StartVM(ctx context.Context, instanceID string) error {
	dom, err := h.lookup(instanceID)
	if err != nil {
		return err
	}
	defer dom.Free()

	if err := dom.Create(); err != nil {
		return fmt.Errorf("start domain %s: %w", instanceID, err)
	}
	return nil
}

func (h *LibvirtHypervisor) StopVM(ctx context.Context, instanceID string) error {
	dom, err := h.lookup(instanceID)
	if err != nil {
		return err
	}
	defer dom.Free()

	// Destroy (hard power-off), not Shutdown (ACPI) — these domains are
	// intentionally diskless, so there's no guest OS to receive or act on
	// an ACPI shutdown signal.
	if err := dom.Destroy(); err != nil {
		return fmt.Errorf("stop domain %s: %w", instanceID, err)
	}
	return nil
}

func (h *LibvirtHypervisor) DeleteVM(ctx context.Context, instanceID string) error {
	dom, err := h.lookup(instanceID)
	if err != nil {
		if errors.Is(err, ErrVMNotFound) {
			return nil // idempotent, same as Store.Delete's no-op-on-missing
		}
		return err
	}
	defer dom.Free()

	if active, _ := dom.IsActive(); active {
		if err := dom.Destroy(); err != nil {
			return fmt.Errorf("destroy domain %s before undefine: %w", instanceID, err)
		}
	}
	if err := dom.Undefine(); err != nil {
		return fmt.Errorf("undefine domain %s: %w", instanceID, err)
	}
	return nil
}

func (h *LibvirtHypervisor) GetVMStatus(ctx context.Context, instanceID string) (VM, error) {
	dom, err := h.lookup(instanceID)
	if err != nil {
		return VM{}, err
	}
	defer dom.Free()

	state, _, err := dom.GetState()
	if err != nil {
		return VM{}, fmt.Errorf("get state of domain %s: %w", instanceID, err)
	}
	info, err := dom.GetInfo()
	if err != nil {
		return VM{}, fmt.Errorf("get info of domain %s: %w", instanceID, err)
	}

	vm := VM{
		InstanceID: instanceID,
		Status:     VMStatusStopped,
		CPU:        int(info.NrVirtCpu),
		MemoryMB:   int(info.MaxMem / 1024),
	}
	if state == libvirtgo.DOMAIN_RUNNING {
		vm.Status = VMStatusRunning
	}

	if metaXML, err := dom.GetMetadata(libvirtgo.DOMAIN_METADATA_ELEMENT, nebulaMetadataNS, libvirtgo.DOMAIN_AFFECT_CURRENT); err == nil {
		var meta nebulaInfoIn
		if err := xml.Unmarshal([]byte(metaXML), &meta); err == nil {
			vm.DiskGB = meta.DiskGB
			vm.Image = meta.Image
		}
	}

	return vm, nil
}

func (h *LibvirtHypervisor) CountVMs(ctx context.Context) (int, error) {
	domains, err := h.conn.ListAllDomains(0)
	if err != nil {
		return 0, fmt.Errorf("list domains: %w", err)
	}
	for _, d := range domains {
		d.Free()
	}
	return len(domains), nil
}
