package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/milosursulovic/nebula/internal/network"
)

type createNetworkRequest struct {
	Name    string `json:"name"`
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
}

type subnetResponse struct {
	ID      string `json:"id"`
	CIDR    string `json:"cidr"`
	Gateway string `json:"gateway"`
}

type networkResponse struct {
	ID      string           `json:"id"`
	Name    string           `json:"name"`
	Subnets []subnetResponse `json:"subnets,omitempty"`
}

func newSubnetResponse(s network.Subnet) subnetResponse {
	return subnetResponse{ID: s.ID, CIDR: s.CIDR, Gateway: s.Gateway}
}

func newNetworkResponse(n network.Network, subnets []network.Subnet) networkResponse {
	resp := networkResponse{ID: n.ID, Name: n.Name}
	for _, s := range subnets {
		resp.Subnets = append(resp.Subnets, newSubnetResponse(s))
	}
	return resp
}

// handleCreateNetwork creates a network and its one subnet (spec section
// 31's single worked example bundles them — see network.Service.
// CreateNetwork's doc comment) in one call.
func handleCreateNetwork(svc network.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createNetworkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, r, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}

		req.Name = strings.TrimSpace(req.Name)
		req.CIDR = strings.TrimSpace(req.CIDR)
		req.Gateway = strings.TrimSpace(req.Gateway)

		if req.Name == "" || req.CIDR == "" || req.Gateway == "" {
			writeError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "name, cidr, gateway are required")
			return
		}

		n, sn, err := svc.CreateNetwork(r.Context(), req.Name, req.CIDR, req.Gateway)
		if err != nil {
			switch {
			case errors.Is(err, network.ErrNameTaken):
				writeError(w, r, http.StatusConflict, "NAME_TAKEN", "a network with this name already exists")
			case errors.Is(err, network.ErrInvalidCIDR):
				writeError(w, r, http.StatusBadRequest, "INVALID_CIDR", "cidr must be a valid network address and gateway must be inside it")
			case errors.Is(err, network.ErrSubnetTooLarge):
				writeError(w, r, http.StatusBadRequest, "SUBNET_TOO_LARGE", "subnet must be /16 or smaller")
			default:
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to create network")
			}
			return
		}

		writeJSON(w, http.StatusCreated, newNetworkResponse(n, []network.Subnet{sn}))
	}
}

// handleListNetworks fetches each network's subnets too (same shape as
// handleGetNetwork) — it used to pass nil, which rendered every
// cidr/gateway as blank in any client that only ever calls list (the
// CLI's `nebula network list` always showed `-`). SUPER_ADMIN-only,
// platform-infrastructure-scale list — one extra query per network is a
// non-issue at that cardinality, so this stays a plain loop rather than
// a repository-level join.
func handleListNetworks(svc network.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		networks, err := svc.List(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list networks")
			return
		}

		resp := make([]networkResponse, 0, len(networks))
		for _, n := range networks {
			subnets, err := svc.SubnetsByNetwork(r.Context(), n.ID)
			if err != nil {
				writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list networks")
				return
			}
			resp = append(resp, newNetworkResponse(n, subnets))
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleGetNetwork(svc network.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")

		n, err := svc.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, network.ErrNotFound) {
				writeError(w, r, http.StatusNotFound, "NETWORK_NOT_FOUND", "no network with this id")
				return
			}
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get network")
			return
		}

		subnets, err := svc.SubnetsByNetwork(r.Context(), id)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to get network")
			return
		}

		writeJSON(w, http.StatusOK, newNetworkResponse(n, subnets))
	}
}
