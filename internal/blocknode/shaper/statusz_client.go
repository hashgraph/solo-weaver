// SPDX-License-Identifier: Apache-2.0

package shaper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/joomcode/errorx"
)

// statuszClientTimeout bounds a single statusz request. It is deliberately
// shorter than the poll loop's 5 s steady-state cadence so a hung BN response
// fails fast and surfaces to the outage policy rather than stacking requests.
const statuszClientTimeout = 4 * time.Second

// statusz endpoint paths, relative to the client's base URL. The transport is
// REST/JSON: network-data.proto defines only the payload shape, not a gRPC
// service.
const (
	inboundClientsPath  = "statusz/inbound-clients"
	outboundClientsPath = "statusz/outbound-clients"
)

// The types below mirror the JSON encoding of
// block-node/api/network-data.proto. They are the decode boundary: the poll
// loop consumes these types via the client, never raw JSON, so coupling to the
// (provisional) BN schema stays in this one file. json.Decode drops the proto
// fields the traffic-shaper doesn't consume (scheme, protocol, certificate), so
// they are simply absent here rather than modelled and thrown away.

// Endpoint is one side (local or remote) of a NetworkConnection. Port is a
// string because the BN reports "*" (any port) alongside numeric ports, and
// Address may be a single IP or a CIDR (e.g. "10.10.1.0/24").
type Endpoint struct {
	Address string `json:"address"`
	Port    string `json:"port"`
}

// NetworkConnection is one active endpoint reported by a statusz endpoint.
// Category is left as the raw BN string here — mapping categories to policy
// names and nft sets is a separate concern from reading the endpoint.
type NetworkConnection struct {
	Local       Endpoint `json:"local"`
	Remote      Endpoint `json:"remote"`
	Category    string   `json:"category"`
	TLSRequired bool     `json:"tls_required"`
}

// NetworkData is the decoded payload of a statusz endpoint: the set of active
// endpoints the BN currently reports.
type NetworkData struct {
	ActiveEndpoints []NetworkConnection `json:"active_endpoints"`
}

// StatuszClient reads a Block Node's statusz REST/JSON endpoints.
type StatuszClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewStatuszClient returns a StatuszClient that reads statusz endpoints under
// baseURL (e.g. "http://10.0.0.5:8080"), using an HTTP client with a bounded
// per-request timeout.
func NewStatuszClient(baseURL string) *StatuszClient {
	return &StatuszClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: statuszClientTimeout},
	}
}

// InboundClients fetches GET {base}/statusz/inbound-clients and decodes the
// NetworkData payload.
func (c *StatuszClient) InboundClients(ctx context.Context) (NetworkData, error) {
	return c.fetch(ctx, inboundClientsPath)
}

// OutboundClients fetches GET {base}/statusz/outbound-clients and decodes the
// NetworkData payload.
func (c *StatuszClient) OutboundClients(ctx context.Context) (NetworkData, error) {
	return c.fetch(ctx, outboundClientsPath)
}

// fetch performs the GET, checks the status, and decodes the body. Every
// failure mode returns a wrapped error (never a panic) so the poll loop can hand
// the fault to the outage policy: a bad base URL is IllegalArgument and an
// unbuildable request is InternalError (both configuration faults), a transport
// failure or non-200 response is ExternalError, and a malformed body is
// IllegalFormat.
func (c *StatuszClient) fetch(ctx context.Context, path string) (NetworkData, error) {
	endpoint, err := url.JoinPath(c.baseURL, path)
	if err != nil {
		return NetworkData{}, errorx.IllegalArgument.Wrap(err, "build statusz URL from base %q", c.baseURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return NetworkData{}, errorx.InternalError.Wrap(err, "build statusz request for %s", path)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return NetworkData{}, errorx.ExternalError.Wrap(err, "fetch statusz %s", path)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return NetworkData{}, errorx.ExternalError.New("statusz %s returned status %d", path, resp.StatusCode)
	}

	var data NetworkData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return NetworkData{}, errorx.IllegalFormat.Wrap(err, "decode statusz %s response", path)
	}
	return data, nil
}
