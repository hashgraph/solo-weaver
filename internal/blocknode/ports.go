// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"fmt"
	oslib "os"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// DefaultBlockNodeHealthPort is the upstream hiero-block-node chart's
// `blockNode.ports.health` default — the /healthz + statusz port
// (oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server). It is the one
// block-node facility port solo-weaver pins a-priori rather than reading back
// from statusz: it bootstraps discovery (you fetch statusz *on* it, so it can't
// itself come from the statusz payload) and it is the port the bn-health policy
// drops. The same value is templated into the rendered chart values
// (blockNode.ports.health) as an explicit default, so absent any operator
// override the port solo-weaver drops and the port the BN listens on can never
// diverge, and the traffic-shaper daemon builds its statusz base URL on it.
//
// The subscriber / block-access / server-status listener ports are not pinned:
// the daemon reconciles them into the inet weaver-workload-policy `<name>_ports`
// sets from the BN's statusz `local.port` at runtime.
const DefaultBlockNodeHealthPort = "40983"

// DefaultBlockNodeServicePort is the upstream hiero-block-node chart's
// `service.port` default — the main server port, which every facility without
// its own `blockNode.ports.<facility>` entry shares. solo-weaver's base values
// set the same value.
const DefaultBlockNodeServicePort = "40840"

// ResolveHealthPort returns the block-node health/statusz port solo-weaver should
// drop off-node (bn-health) and, later, dial for statusz. It reads
// `blockNode.ports.health` from the operator's effective --values file when one
// is supplied, so the dropped port follows whatever the operator configured the
// BN to listen on rather than a value baked into solo-weaver; it falls back to
// DefaultBlockNodeHealthPort (which solo-weaver also templates into its base
// values as the explicit default) when the file omits it or none is supplied.
//
// Only `blockNode.ports.health` is consulted — not `blockNode.config.SERVER_PORT`,
// which is the main gRPC/service port (a different port from the /healthz +
// statusz endpoint); using it as a fallback would point bn-health at the wrong
// port whenever an operator overrode SERVER_PORT but left the health port at its
// chart default.
func ResolveHealthPort(valuesFile string) (string, error) {
	if valuesFile == "" {
		return DefaultBlockNodeHealthPort, nil
	}
	data, err := oslib.ReadFile(valuesFile)
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "read block node values file %s", valuesFile)
	}
	var v struct {
		BlockNode struct {
			Ports struct {
				Health any `yaml:"health"`
			} `yaml:"ports"`
		} `yaml:"blockNode"`
	}
	if err := yaml.Unmarshal(data, &v); err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "parse block node values file %s", valuesFile)
	}
	if p := scalarPort(v.BlockNode.Ports.Health); p != "" {
		return p, nil
	}
	return DefaultBlockNodeHealthPort, nil
}

// ResolvePublisherPort returns the port the block node serves its publish API
// on, which bn-publisher restricts to known publishers. It reads
// `blockNode.ports.publisher` from the operator's --values file, then from
// releaseValues, falls back to `service.port` from the file (the chart shares
// that port with every facility left at null), and then to
// DefaultBlockNodeServicePort.
//
// releaseValues are the deployed release's user-supplied values when the chart
// upgrade reuses them, else nil. Helm keeps a reused key the new values leave
// unset and deletes one they set to null, so an explicit null in the file skips
// the release. The release's service.port is never consulted: the base values
// template pins it, so the reused value is always overridden.
//
// It is pinned from the values rather than read from statusz because statusz
// reports the publish port only alongside a configured publisher: with an empty
// roster the port would be unknown and the publish port left unguarded.
func ResolvePublisherPort(valuesFile string, releaseValues map[string]any) (string, error) {
	file := map[string]any{}
	if valuesFile != "" {
		data, err := oslib.ReadFile(valuesFile)
		if err != nil {
			return "", errorx.ExternalError.Wrap(err, "read block node values file %s", valuesFile)
		}
		if err := yaml.Unmarshal(data, &file); err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "parse block node values file %s", valuesFile)
		}
	}
	if v, set := lookupValue(file, "blockNode", "ports", "publisher"); set {
		if p := scalarPort(v); p != "" {
			return p, nil
		}
	} else if v, _ := lookupValue(releaseValues, "blockNode", "ports", "publisher"); scalarPort(v) != "" {
		return scalarPort(v), nil
	}
	if v, _ := lookupValue(file, "service", "port"); scalarPort(v) != "" {
		return scalarPort(v), nil
	}
	return DefaultBlockNodeServicePort, nil
}

// lookupValue walks a nested values map along path. It reports whether the last
// key is present, since a present key may hold null.
func lookupValue(values map[string]any, path ...string) (any, bool) {
	cur := values
	for i, key := range path {
		v, ok := cur[key]
		if !ok {
			return nil, false
		}
		if i == len(path)-1 {
			return v, true
		}
		if cur, ok = v.(map[string]any); !ok {
			return nil, false
		}
	}
	return nil, false
}

// scalarPort renders a YAML scalar port value (which may decode as an int or a
// string) as a string, or "" when unset/null so the caller falls back to the
// default. Any invalid value is left for the policy layer's ValidatePort to
// reject with a precise error at create time.
func scalarPort(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		return fmt.Sprintf("%d", int64(t))
	default:
		return fmt.Sprintf("%v", t)
	}
}
