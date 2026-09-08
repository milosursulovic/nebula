package main

import (
	"fmt"
	"os"
)

type command func(args []string) error

// commands is the noun -> verb -> handler table for every command spec
// section 49 names (login is dispatched separately below — it has no verb).
var commands = map[string]map[string]command{
	"tenant":   {"list": cmdTenantList},
	"node":     {"list": cmdNodeList, "get": cmdNodeGet, "drain": cmdNodeDrain},
	"instance": {"create": cmdInstanceCreate, "list": cmdInstanceList, "get": cmdInstanceGet, "start": cmdInstanceStart, "stop": cmdInstanceStop, "delete": cmdInstanceDelete},
	"network":  {"list": cmdNetworkList, "create": cmdNetworkCreate},
	"job":      {"list": cmdJobList, "retry": cmdJobRetry},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	noun := args[0]
	if noun == "login" || noun == "help" || noun == "--help" || noun == "-h" {
		if noun == "login" {
			return cmdLogin(args[1:])
		}
		printUsage()
		return nil
	}

	verbs, ok := commands[noun]
	if !ok {
		return fmt.Errorf("unknown command %q — run `nebula help`", noun)
	}
	if len(args) < 2 {
		return fmt.Errorf("%s requires a subcommand — run `nebula help`", noun)
	}

	verb := args[1]
	cmd, ok := verbs[verb]
	if !ok {
		return fmt.Errorf("unknown command %q %q — run `nebula help`", noun, verb)
	}
	return cmd(args[2:])
}

func printUsage() {
	fmt.Println(`nebula — the NEBULA CLI (spec section 49)

Usage:
  nebula login <email> <password>

  nebula tenant list

  nebula node list
  nebula node get <id>
  nebula node drain <id>

  nebula instance create --name NAME --cpu N --memory MB --disk GB --image IMAGE
  nebula instance list
  nebula instance get <id>
  nebula instance start <id>
  nebula instance stop <id>
  nebula instance delete <id>

  nebula network list
  nebula network create <name> --cidr CIDR --gateway GATEWAY

  nebula job list [--status STATUS]
  nebula job retry <id>

NEBULA_API_URL sets the API base (default http://localhost:8080).
Credentials from login are stored in ~/.nebula/credentials.json.`)
}
