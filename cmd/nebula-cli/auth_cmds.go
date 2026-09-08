package main

import (
	"fmt"
	"os"
)

func defaultAPIURL() string {
	if v := os.Getenv("NEBULA_API_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func cmdLogin(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: nebula login <email> <password>")
	}
	email, password := args[0], args[1]

	c := newClient(credentials{APIURL: defaultAPIURL()})

	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.do("POST", "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	}, &tokens); err != nil {
		return err
	}

	if err := saveCredentials(credentials{
		APIURL:       c.creds.APIURL,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	}); err != nil {
		return err
	}

	fmt.Println("logged in")
	return nil
}
