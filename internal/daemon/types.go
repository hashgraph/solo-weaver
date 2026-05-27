// SPDX-License-Identifier: Apache-2.0

package daemon

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is returned by all error paths to keep Content-Type consistent.
type ErrorResponse struct {
	Error string `json:"error"`
}
