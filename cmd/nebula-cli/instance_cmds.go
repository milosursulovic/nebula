package main

import (
	"flag"
	"fmt"
)

// instanceResponse mirrors pkg/api/instance_handlers.go's instanceResponse.
type instanceResponse struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPU       int     `json:"cpu"`
	MemoryMB  int     `json:"memory_mb"`
	DiskGB    int     `json:"disk_gb"`
	Image     string  `json:"image"`
	NodeID    *string `json:"node_id"`
	IPAddress *string `json:"ip_address"`
}

func cmdInstanceCreate(args []string) error {
	fs := flag.NewFlagSet("instance create", flag.ContinueOnError)
	name := fs.String("name", "", "instance name (required)")
	cpu := fs.Int("cpu", 0, "vCPUs (required)")
	memory := fs.Int("memory", 0, "memory in MB (required)")
	disk := fs.Int("disk", 0, "disk size in GB (required)")
	image := fs.String("image", "", "image name (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" || *cpu <= 0 || *memory <= 0 || *disk <= 0 || *image == "" {
		return fmt.Errorf("usage: nebula instance create --name NAME --cpu N --memory MB --disk GB --image IMAGE")
	}

	c, err := authedClient()
	if err != nil {
		return err
	}

	var i instanceResponse
	if err := c.do("POST", "/api/v1/instances", map[string]any{
		"name": *name, "cpu": *cpu, "memory_mb": *memory, "disk_gb": *disk, "image": *image,
	}, &i); err != nil {
		return err
	}

	fmt.Printf("instance %s created (status: %s)\n", i.ID, i.Status)
	return nil
}

func cmdInstanceList(args []string) error {
	c, err := authedClient()
	if err != nil {
		return err
	}

	var instances []instanceResponse
	if err := c.do("GET", "/api/v1/instances", nil, &instances); err != nil {
		return err
	}

	w := newTable()
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tCPU\tMEMORY_MB\tDISK_GB\tNODE_ID\tIP_ADDRESS")
	for _, i := range instances {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			i.ID, i.Name, i.Status, i.CPU, i.MemoryMB, i.DiskGB, deref(i.NodeID), deref(i.IPAddress))
	}
	return w.Flush()
}

func cmdInstanceGet(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: nebula instance get <id>")
	}
	c, err := authedClient()
	if err != nil {
		return err
	}

	var i instanceResponse
	if err := c.do("GET", "/api/v1/instances/"+args[0], nil, &i); err != nil {
		return err
	}

	printKV(
		"ID", i.ID,
		"Name", i.Name,
		"Status", i.Status,
		"CPU", fmt.Sprintf("%d", i.CPU),
		"Memory MB", fmt.Sprintf("%d", i.MemoryMB),
		"Disk GB", fmt.Sprintf("%d", i.DiskGB),
		"Image", i.Image,
		"Node ID", deref(i.NodeID),
		"IP Address", deref(i.IPAddress),
	)
	return nil
}

func cmdInstanceStart(args []string) error {
	return instanceLifecycleAction(args, "start", "started")
}

func cmdInstanceStop(args []string) error {
	return instanceLifecycleAction(args, "stop", "stopped")
}

func instanceLifecycleAction(args []string, verb, pastTense string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: nebula instance %s <id>", verb)
	}
	c, err := authedClient()
	if err != nil {
		return err
	}

	var i instanceResponse
	if err := c.do("POST", "/api/v1/instances/"+args[0]+"/"+verb, nil, &i); err != nil {
		return err
	}

	fmt.Printf("instance %s %s\n", i.ID, pastTense)
	return nil
}

func cmdInstanceDelete(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: nebula instance delete <id>")
	}
	c, err := authedClient()
	if err != nil {
		return err
	}

	var i instanceResponse
	if err := c.do("DELETE", "/api/v1/instances/"+args[0], nil, &i); err != nil {
		return err
	}

	fmt.Printf("instance %s deleted\n", i.ID)
	return nil
}
