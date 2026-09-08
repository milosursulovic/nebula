package main

import "fmt"

type tenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func cmdTenantList(args []string) error {
	c, err := authedClient()
	if err != nil {
		return err
	}

	var tenants []tenantResponse
	if err := c.do("GET", "/api/v1/tenants", nil, &tenants); err != nil {
		return err
	}

	w := newTable()
	fmt.Fprintln(w, "ID\tNAME\tCREATED_AT")
	for _, t := range tenants {
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.ID, t.Name, t.CreatedAt)
	}
	return w.Flush()
}
