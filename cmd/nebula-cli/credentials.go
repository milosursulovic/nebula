package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// credentials is what `nebula login` persists to disk so every later
// command can authenticate without asking again.
type credentials struct {
	APIURL       string `json:"api_url"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func credentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".nebula", "credentials.json"), nil
}

func loadCredentials() (credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return credentials{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return credentials{}, fmt.Errorf("not logged in — run `nebula login <email> <password>` first")
		}
		return credentials{}, err
	}

	var creds credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return credentials{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return creds, nil
}

// saveCredentials writes creds to ~/.nebula/credentials.json, mode 0600 —
// it holds a live bearer token, same sensitivity as an SSH key.
func saveCredentials(creds credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
