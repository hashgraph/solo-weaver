// SPDX-License-Identifier: Apache-2.0

package common

import (
	"strconv"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// FlagNameShape is the CLI flag name for the per-class HTB bandwidth
// overrides applied by `block node install`.
const FlagNameShape = "shape"

// ParseShapeOverrides parses repeated --shape values of the form
// `<class>=rate=<r>,ceil=<c>,prio=<p>` into per-class overrides keyed by class
// name. Each --shape occurrence carries one class; any subset of rate/ceil/prio
// may be given (omitted fields keep the profile default). Every override is
// validated against the known classes and tc-rate/prio rules, so a bad value is
// rejected before any install work runs. The returned map is nil when raw is
// empty.
func ParseShapeOverrides(raw []string) (map[string]models.ShapeOverride, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]models.ShapeOverride, len(raw))
	for _, entry := range raw {
		class, spec, ok := strings.Cut(strings.TrimSpace(entry), "=")
		class = strings.TrimSpace(class)
		if !ok || class == "" || strings.TrimSpace(spec) == "" {
			return nil, errorx.IllegalArgument.New(
				"invalid --%s %q: expected <class>=rate=<r>,ceil=<c>,prio=<p>", FlagNameShape, entry)
		}
		if _, dup := out[class]; dup {
			return nil, errorx.IllegalArgument.New("duplicate --%s override for class %q", FlagNameShape, class)
		}

		var o models.ShapeOverride
		for _, kv := range strings.Split(spec, ",") {
			key, val, ok := strings.Cut(strings.TrimSpace(kv), "=")
			key, val = strings.TrimSpace(key), strings.TrimSpace(val)
			if !ok || val == "" {
				return nil, errorx.IllegalArgument.New(
					"invalid --%s field %q for class %q: expected key=value", FlagNameShape, kv, class)
			}
			switch key {
			case "rate":
				o.Rate = val
			case "ceil":
				o.Ceil = val
			case "prio":
				n, err := strconv.Atoi(val)
				if err != nil {
					return nil, errorx.IllegalArgument.New(
						"invalid --%s prio %q for class %q: must be an integer in [0,7]", FlagNameShape, val, class)
				}
				o.Prio = &n
			default:
				return nil, errorx.IllegalArgument.New(
					"unknown --%s field %q for class %q: expected rate, ceil, or prio", FlagNameShape, key, class)
			}
		}

		if err := shape.ValidateClassOverride(class, shape.ClassOverride{Rate: o.Rate, Ceil: o.Ceil, Prio: o.Prio}); err != nil {
			return nil, errorx.Decorate(err, "invalid --%s override for class %q", FlagNameShape, class)
		}
		out[class] = o
	}
	return out, nil
}
