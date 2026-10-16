// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"strings"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// ParseAllowedOrigins parses a comma-separated allowlist of permitted origins,
// for example "provisioned" or "provisioned,external". An empty string returns
// nil, meaning "no restriction" — the gate's own verdict stands. An unknown token
// is an error, so a typo fails closed rather than silently allowing everything.
//
// The allowlist can only narrow what is permitted: it is applied after the gate
// has independently classified the target, and it can only turn an allowed origin
// into a refusal, never the reverse. It cannot turn a stop into a pass.
func ParseAllowedOrigins(s string) ([]Origin, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	var out []Origin
	for _, tok := range strings.Split(s, ",") {
		switch strings.ToLower(strings.TrimSpace(tok)) {
		case "":
			continue
		case Provisioned.String():
			out = append(out, Provisioned)
		case External.String():
			out = append(out, External)
		default:
			return nil, errorx.IllegalArgument.New(
				"unknown origin %q; allowed: %s, %s", tok, Provisioned, External)
		}
	}
	return out, nil
}

// OriginAllowed reports whether origin is permitted by allowed. An empty allowed
// list means no restriction (everything the gate passed is allowed).
func OriginAllowed(allowed []Origin, origin Origin) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == origin {
			return true
		}
	}
	return false
}

// RestrictedError builds the refusal returned when the gate's verdict is not in
// the operator's allowlist. It is a policy refusal, not a check failure.
func RestrictedError(allowed []Origin, origin Origin) error {
	names := make([]string, 0, len(allowed))
	for _, a := range allowed {
		names = append(names, a.String())
	}
	return errx.Decorate(
		errorx.RejectedOperation.New(
			"origin %q is not permitted here (allowed: %s)", origin, strings.Join(names, ", ")),
		reasons.PreconditionNotMet,
		"This host restricts which cluster origins weaver may operate in (SOLO_PROVISIONER_ALLOWED_ORIGINS).",
		"Operate in an allowed origin, or change the allowlist if this restriction is wrong.")
}
