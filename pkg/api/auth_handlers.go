package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/milosursulovic/nebula/internal/auth"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func newTokenResponse(t auth.TokenPair) tokenResponse {
	return tokenResponse{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    t.ExpiresIn,
	}
}

type registerRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TenantName string `json:"tenant_name"`
}

func handleRegister(svc auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))
		req.TenantName = strings.TrimSpace(req.TenantName)

		if req.Email == "" || req.TenantName == "" {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "email and tenant_name are required")
			return
		}
		if len(req.Password) < 8 {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "password must be at least 8 characters")
			return
		}

		tokens, err := svc.Register(r.Context(), req.Email, req.Password, req.TenantName)
		if err != nil {
			if errors.Is(err, auth.ErrEmailTaken) {
				writeError(w, r, http.StatusConflict, "EMAIL_TAKEN", "an account with this email already exists")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to register")
			return
		}

		writeJSON(w, http.StatusCreated, newTokenResponse(tokens))
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleLogin(svc auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}

		req.Email = strings.TrimSpace(strings.ToLower(req.Email))

		tokens, err := svc.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrNoMembership) {
				writeError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to log in")
			return
		}

		writeJSON(w, http.StatusOK, newTokenResponse(tokens))
	}
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func handleRefresh(svc auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
			return
		}

		tokens, err := svc.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			if errors.Is(err, auth.ErrRefreshTokenInvalid) || errors.Is(err, auth.ErrNoMembership) {
				writeError(w, r, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "refresh token is invalid or expired")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to refresh token")
			return
		}

		writeJSON(w, http.StatusOK, newTokenResponse(tokens))
	}
}

func handleLogout(svc auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req refreshRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "refresh_token is required")
			return
		}

		if err := svc.Logout(r.Context(), req.RefreshToken); err != nil {
			if errors.Is(err, auth.ErrRefreshTokenInvalid) {
				writeError(w, r, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "refresh token is invalid or expired")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to log out")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
