package main

import (
	"flag"
	"fmt"
)

// subnetResponse / networkResponse mirror pkg/api/network_handlers.go's.
type subnetResponse struct {
	ID      string `json:"id"`
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
}

type networkResponse struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Subnets []subnetResponse `json:"subnets,omitempty"`
}

func cmdNetworkList(args []string) error {
	c, err := authedClient()
	if err != nil {
		return err
	}

	var networks []networkResponse
	if err := c.do("GET", "/api/v1/networks", nil, &networks); err != nil {
		return err
	}

	w := newTable()
	fmt.Fprintln(w, "ID\tNAME\tCIDR\tGATEWAY")
	for _, n := range networks {
		cidr, gateway := "-", "-"
		if len(n.Subnets) > 0 {
			cidr, gateway = n.Subnets[0].CIDR, n.Subnets[0].Gateway
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.ID, n.Name, cidr, gateway)
	}
	return w.Flush()
}

// cmdNetworkCreate takes its name as a leading positional argument, then
// flags — `nebula network create production --cidr ... --gateway ...`
// (spec section 49's own example order). Go's flag package stops parsing
// at the first non-flag argument, so the name is peeled off by hand before
// handing the rest to a FlagSet.
func cmdNetworkCreate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nebula network create <name> --cidr CIDR --gateway GATEWAY")
	}
	name := args[0]

	fs := flag.NewFlagSet("network create", flag.ContinueOnError)
	cidr := fs.String("cidr", "", "subnet CIDR, e.g. 10.20.0.0/24 (required)")
	gateway := fs.String("gateway", "", "gateway IP (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *cidr == "" || *gateway == "" {
		return fmt.Errorf("usage: nebula network create <name> --cidr CIDR --gateway GATEWAY")
	}

	c, err := authedClient()
	if err != nil {
		return err
	}

	var n networkResponse
	if err := c.do("POST", "/api/v1/networks", map[string]string{
		"name": name, "cidr": *cidr, "gateway": *gateway,
	}, &n); err != nil {
		return err
	}

	fmt.Printf("network %s created\n", n.ID)
	return nil
}
