package main

import (
	"fmt"
	"os"
	"text/tabwriter"
)

// newTable returns a tabwriter already wired to stdout — callers write a
// header row, then one row per item, then must call Flush.
func newTable() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

func printKV(pairs ...string) {
	if len(pairs)%2 != 0 {
		panic("printKV: odd number of arguments")
	}
	w := newTable()
	for i := 0; i < len(pairs); i += 2 {
		fmt.Fprintf(w, "%s:\t%s\n", pairs[i], pairs[i+1])
	}
	w.Flush()
}

func deref(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}
