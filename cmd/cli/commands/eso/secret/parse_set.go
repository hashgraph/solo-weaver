// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"regexp"
	"strings"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

// remoteRefValuePattern bounds the store path and property to a charset without
// `"` or `\`, so values cannot escape the quoted scalars of the rendered manifest.
var remoteRefValuePattern = regexp.MustCompile(`^[A-Za-z0-9._/@+=-]+$`)

// parseSetFlags parses repeatable --set values of the form KEY=store/path[#field]
// into ExternalSecret data entries, validating each field. At least one value is
// required. It returns an IllegalArgument error on missing, malformed, or unsafe
// input.
func parseSetFlags(flags []string) ([]models.ESOSecretDataEntry, error) {
	if len(flags) == 0 {
		return nil, errorx.IllegalArgument.New("at least one --set KEY=store/path[#field] is required")
	}

	entries := make([]models.ESOSecretDataEntry, 0, len(flags))
	seenKeys := make(map[string]struct{}, len(flags))
	for _, flag := range flags {
		rawKey, rawValue, found := strings.Cut(flag, "=")
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)
		if !found || key == "" {
			return nil, errorx.IllegalArgument.New("invalid --set %q, expected KEY=store/path[#field]", flag)
		}

		if err := sanity.ValidateIdentifier(key); err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "invalid secret key %q in --set %q", key, flag)
		}

		// Duplicate keys would render duplicate secretKey entries, which ESO's webhook rejects; fail fast here.
		if _, dup := seenKeys[key]; dup {
			return nil, errorx.IllegalArgument.New("duplicate secret key %q in --set %q", key, flag)
		}
		seenKeys[key] = struct{}{}

		remoteKey, property, _ := strings.Cut(value, "#")

		if !remoteRefValuePattern.MatchString(remoteKey) {
			return nil, errorx.IllegalArgument.New("invalid store path %q in --set %q", remoteKey, flag)
		}
		if property != "" && !remoteRefValuePattern.MatchString(property) {
			return nil, errorx.IllegalArgument.New("invalid property %q in --set %q", property, flag)
		}

		entries = append(entries, models.ESOSecretDataEntry{
			SecretKey: key,
			RemoteKey: remoteKey,
			Property:  property,
		})
	}

	return entries, nil
}
