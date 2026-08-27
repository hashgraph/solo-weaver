// SPDX-License-Identifier: Apache-2.0

package sanity

import (
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Remediation hints, one per validated domain: each states the accepted form.
const (
	hintIdentifier  = "Use only letters, digits, underscore or hyphen ([A-Za-z0-9_-])."
	hintDNSName     = "Use a hostname of letters, digits, dots and hyphens, e.g. node1.example.com."
	hintHexToken    = "Supply the token exactly as issued: hex digits only (0-9, a-f, A-F), at most 4096 characters."
	hintHostPort    = "Use host or host:port, e.g. teleport.example.com:3080 — no scheme, no path."
	hintUsername    = "Use an existing Linux account name ([A-Za-z0-9_.-]), e.g. hedera or first.last."
	hintOperationID = "Use only letters, digits, dot, underscore or hyphen, e.g. upgrade-v0.76.0-20060102T150405Z."
	hintPath        = "Use an absolute path with no '..' segments or shell metacharacters, e.g. /opt/solo/weaver."
	hintVersion     = "Use a semantic version, e.g. 1.2.3, 1.2.3-beta.1."
	hintChartRef    = "Use oci://registry/path/chart, an https:// chart URL, or repo/chart-name."
	hintStorageSize = "Use a Kubernetes quantity greater than zero with unit Gi, Mi or Ti, e.g. 500Gi."
	hintCIDR        = "Use CIDR notation with an explicit prefix length, e.g. 10.0.0.0/8."
	hintIPv4CIDR    = "Use an IPv4 CIDR, e.g. 10.0.0.0/8 — IPv6 is not supported here."
	hintFQDN        = "Use a fully-qualified domain name with at least one dot, e.g. jump.corp.example.com."
	hintPort        = "Use a TCP/UDP port number between 1 and 65535."
	hintInputFile   = "Check the path and that the file exists and is a regular file."

	// Split because ValidateURL's accepted schemes depend on opts.AllowHTTP.
	hintURL        = "Use a full URL with an ASCII hostname, e.g. https://registry.example.com/path."
	hintURLHTTPS   = "Use an https:// URL."
	hintURLPlainOK = "Use an http:// or https:// URL."
	hintFileDenied = "Check the permissions on the file and its parent directories, or re-run with sudo."
)

// ErrInvalidName is returned by Sanitize* helpers when the input contains
// no valid characters and would otherwise sanitize to an empty string.
var ErrInvalidName = errx.Decorate(
	errorx.IllegalArgument.New("invalid name"),
	reasons.InvalidArgument,
	hintIdentifier,
)

// invalidArgf builds the standard rejection: IllegalArgument + InvalidArgument reason + hint.
func invalidArgf(hint, format string, args ...any) error {
	return errx.Decorate(
		errorx.IllegalArgument.New(format, args...),
		reasons.InvalidArgument,
		hint,
	)
}

// invalidArgWrapf is invalidArgf over a cause, whose hint it shadows.
func invalidArgWrapf(hint string, cause error, format string, args ...any) error {
	return errx.Decorate(
		errorx.IllegalArgument.Wrap(cause, format, args...),
		reasons.InvalidArgument,
		hint,
	)
}
