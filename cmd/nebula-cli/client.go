package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// apiError mirrors pkg/api/errors.go's errorResponse shape (spec section
// 39) — the CLI surfaces its Message directly rather than a raw status
// code, since that's what the API actually put effort into explaining.
type apiError struct {
	Status  int    `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// client is a thin HTTP wrapper around nebula-api's /api/v1 surface. It
// injects the stored access token and, on a single 401, refreshes it via
// the stored refresh token and retries the request once — an access token
// is only good for 15 minutes (internal/auth.AccessTokenTTL), too short for
// a CLI session to babysit by hand.
type client struct {
	httpClient *http.Client
	creds      credentials
}

func newClient(creds credentials) *client {
	return &client{httpClient: &http.Client{Timeout: 30 * time.Second}, creds: creds}
}

// authedClient loads persisted credentials from `nebula login` — every
// command but login itself needs this.
func authedClient() (*client, error) {
	creds, err := loadCredentials()
	if err != nil {
		return nil, err
	}
	return newClient(creds), nil
}

// do sends method/path (path already includes /api/v1/...) with body
// (marshaled as JSON if non-nil) and decodes a 2xx response into out (if
// non-nil). A non-2xx response is returned as *apiError.
func (c *client) do(method, path string, body, out any) error {
	resp, err := c.doOnce(method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && c.creds.RefreshToken != "" {
		if err := c.refresh(); err == nil {
			resp.Body.Close()
			resp, err = c.doOnce(method, path, body)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr apiError
		if jsonErr := json.NewDecoder(resp.Body).Decode(&apiErr); jsonErr != nil || apiErr.Message == "" {
			return fmt.Errorf("request failed: %s", resp.Status)
		}
		return &apiErr
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) doOnce(method, path string, body any) (*http.Response, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, c.creds.APIURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.creds.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	}

	return c.httpClient.Do(req)
}

// refresh exchanges the stored refresh token for a new pair and persists
// it, so the new access token survives to the CLI's next invocation too,
// not just the rest of this one.
func (c *client) refresh() error {
	resp, err := c.doOnce(http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": c.creds.RefreshToken,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed: %s", resp.Status)
	}

	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return err
	}

	c.creds.AccessToken = tokens.AccessToken
	c.creds.RefreshToken = tokens.RefreshToken
	return saveCredentials(c.creds)
}
