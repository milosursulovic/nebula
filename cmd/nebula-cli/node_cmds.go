package main

import "fmt"

// nodeResponse mirrors pkg/api/node_handlers.go's nodeResponse.
type nodeResponse struct {
	ID                string  `json:"id"`
	Hostname          string  `json:"hostname"`
	IP                string  `json:"ip"`
	Status            string  `json:"status"`
	TotalCPU          int     `json:"total_cpu"`
	AvailableCPU      int     `json:"available_cpu"`
	TotalMemoryMB     int     `json:"total_memory_mb"`
	AvailableMemoryMB int     `json:"available_memory_mb"`
	TotalDiskGB       int     `json:"total_disk_gb"`
	AvailableDiskGB   int     `json:"available_disk_gb"`
	LoadAverage       float64 `json:"load_average"`
	RunningInstances  int     `json:"running_instances"`
	LastHeartbeatAt   *string `json:"last_heartbeat_at"`
}

func cmdNodeList(args []string) error {
	c, err := authedClient()
	if err != nil {
		return err
	}

	var nodes []nodeResponse
	if err := c.do("GET", "/api/v1/nodes", nil, &nodes); err != nil {
		return err
	}

	w := newTable()
	fmt.Fprintln(w, "ID\tHOSTNAME\tSTATUS\tCPU\tMEMORY_MB\tDISK_GB\tRUNNING_INSTANCES")
	for _, n := range nodes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%d/%d\t%d/%d\t%d\n",
			n.ID, n.Hostname, n.Status,
			n.TotalCPU-n.AvailableCPU, n.TotalCPU,
			n.TotalMemoryMB-n.AvailableMemoryMB, n.TotalMemoryMB,
			n.TotalDiskGB-n.AvailableDiskGB, n.TotalDiskGB,
			n.RunningInstances,
		)
	}
	return w.Flush()
}

func cmdNodeGet(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: nebula node get <id>")
	}
	c, err := authedClient()
	if err != nil {
		return err
	}

	var n nodeResponse
	if err := c.do("GET", "/api/v1/nodes/"+args[0], nil, &n); err != nil {
		return err
	}

	printKV(
		"ID", n.ID,
		"Hostname", n.Hostname,
		"IP", n.IP,
		"Status", n.Status,
		"CPU used/total", fmt.Sprintf("%d/%d", n.TotalCPU-n.AvailableCPU, n.TotalCPU),
		"Memory MB used/total", fmt.Sprintf("%d/%d", n.TotalMemoryMB-n.AvailableMemoryMB, n.TotalMemoryMB),
		"Disk GB used/total", fmt.Sprintf("%d/%d", n.TotalDiskGB-n.AvailableDiskGB, n.TotalDiskGB),
		"Running instances", fmt.Sprintf("%d", n.RunningInstances),
		"Last heartbeat", deref(n.LastHeartbeatAt),
	)
	return nil
}

func cmdNodeDrain(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: nebula node drain <id>")
	}
	c, err := authedClient()
	if err != nil {
		return err
	}

	var n nodeResponse
	if err := c.do("POST", "/api/v1/nodes/"+args[0]+"/drain", nil, &n); err != nil {
		return err
	}

	fmt.Printf("node %s draining\n", n.ID)
	return nil
}
