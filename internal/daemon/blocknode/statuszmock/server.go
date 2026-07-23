// SPDX-License-Identifier: Apache-2.0

// Package statuszmock provides a minimal mock of a Block Node's statusz REST
// endpoints for daemon development and tests. It serves the two endpoints the
// traffic-shaper monitor polls — `statusz/inbound-clients` and
// `statusz/outbound-clients` — from an editable roster, so a developer can point
// the daemon's local-fallback statusz source at it and watch the nft set
// membership follow roster edits.
//
// The wire contract mirrors block-node/api/network-data.proto (a `NetworkData`
// with `active_endpoints`). It is defined independently here so the mock stays
// decoupled from the daemon's decode types and can serve the contract on its
// own. The category vocabulary is the real BN contract: partner, publisher,
// public, restricted. Peer block nodes this BN backfills from appear as OUTBOUND
// partner endpoints, not a distinct category.
package statuszmock

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/joomcode/errorx"
)

// Endpoint is one side (local or remote) of a Connection. Port is a string
// because the BN reports "*" (any) alongside numeric ports.
type Endpoint struct {
	Address string `json:"address"`
	Port    string `json:"port"`
}

// Connection is one active endpoint the BN reports.
type Connection struct {
	Local       Endpoint `json:"local"`
	Remote      Endpoint `json:"remote"`
	Category    string   `json:"category"`
	TLSRequired bool     `json:"tls_required"`
}

// networkData is the JSON envelope both endpoints return.
type networkData struct {
	ActiveEndpoints []Connection `json:"active_endpoints"`
}

// Roster is the mock's full view: the endpoints served by each statusz endpoint.
type Roster struct {
	Inbound  []Connection `json:"inbound"`
	Outbound []Connection `json:"outbound"`
}

// RosterProvider returns the roster to serve for a given request. A static
// provider returns a fixed roster; a file provider re-reads a JSON file so edits
// are picked up live between polls.
type RosterProvider func() (Roster, error)

// StaticRoster returns a provider that always serves r.
func StaticRoster(r Roster) RosterProvider {
	return func() (Roster, error) { return r, nil }
}

// FileRoster returns a provider that reads and decodes the JSON roster at path
// on every call, so external edits take effect on the next poll without a
// restart. A missing file yields an empty roster (the BN "has no clients yet"
// bootstrap state) rather than an error.
func FileRoster(path string) RosterProvider {
	return func() (Roster, error) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return Roster{}, nil
		}
		if err != nil {
			return Roster{}, errorx.ExternalError.Wrap(err, "read mock statusz roster %s", path)
		}
		var r Roster
		if err := json.Unmarshal(data, &r); err != nil {
			return Roster{}, errorx.IllegalFormat.Wrap(err, "decode mock statusz roster %s", path)
		}
		return r, nil
	}
}

// Handler returns an http.Handler serving the two statusz endpoints from the
// given provider. Paths are matched with and without a leading `statusz/` prefix
// so it works whether mounted at the root or behind a `statusz/` mount.
func Handler(provider RosterProvider) http.Handler {
	mux := http.NewServeMux()
	inbound := func(w http.ResponseWriter, _ *http.Request) {
		serve(w, provider, func(r Roster) []Connection { return r.Inbound })
	}
	outbound := func(w http.ResponseWriter, _ *http.Request) {
		serve(w, provider, func(r Roster) []Connection { return r.Outbound })
	}
	mux.HandleFunc("/statusz/inbound-clients", inbound)
	mux.HandleFunc("/statusz/outbound-clients", outbound)
	mux.HandleFunc("/inbound-clients", inbound)
	mux.HandleFunc("/outbound-clients", outbound)
	return mux
}

func serve(w http.ResponseWriter, provider RosterProvider, pick func(Roster) []Connection) {
	roster, err := provider()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	endpoints := pick(roster)
	if endpoints == nil {
		endpoints = []Connection{}
	}
	w.Header().Set("Content-Type", "application/json")
	// Encoding a small, in-memory struct does not fail in practice; if it did,
	// the header is already sent, so there is nothing further to signal.
	_ = json.NewEncoder(w).Encode(networkData{ActiveEndpoints: endpoints})
}
