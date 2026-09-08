package main

import (
	"flag"
	"fmt"
)

// jobResponse mirrors pkg/api/job_handlers.go's jobResponse.
type jobResponse struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Attempts    int     `json:"attempts"`
	MaxAttempts int     `json:"max_attempts"`
	Error       *string `json:"error"`
}

func cmdJobList(args []string) error {
	fs := flag.NewFlagSet("job list", flag.ContinueOnError)
	status := fs.String("status", "", "filter by status, e.g. FAILED (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := authedClient()
	if err != nil {
		return err
	}

	path := "/api/v1/jobs"
	if *status != "" {
		path += "?status=" + *status
	}

	var jobs []jobResponse
	if err := c.do("GET", path, nil, &jobs); err != nil {
		return err
	}

	w := newTable()
	fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tATTEMPTS\tERROR")
	for _, j := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\t%s\n", j.ID, j.Type, j.Status, j.Attempts, j.MaxAttempts, deref(j.Error))
	}
	return w.Flush()
}

func cmdJobRetry(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: nebula job retry <id>")
	}
	c, err := authedClient()
	if err != nil {
		return err
	}

	var j jobResponse
	if err := c.do("POST", "/api/v1/jobs/"+args[0]+"/retry", nil, &j); err != nil {
		return err
	}

	fmt.Printf("job %s status: %s\n", j.ID, j.Status)
	return nil
}
