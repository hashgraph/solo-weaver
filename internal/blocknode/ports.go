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
// itself come from the statusz payload) and it gates the bn-mgmt policy set. The
// same value is templated into the rendered chart values (blockNode.ports.health)
// as an explicit default, so absent any operator override the port solo-weaver
// allows and the port the BN listens on can never diverge, and the traffic-shaper
// daemon builds its statusz base URL on it.
//
// The publisher / subscriber / block-access / server-status listener ports are
// deliberately NOT pinned here: the daemon reconciles them into the inet weaver
// `<name>_ports` sets from the BN's statusz `local.port` at runtime.
const DefaultBlockNodeHealthPort = "40983"

// ResolveHealthPort returns the block-node health/statusz port solo-weaver should
// allow (bn-mgmt) and, later, dial for statusz. It reads `blockNode.ports.health`
// from the operator's effective --values file when one is supplied, so the
// allowed port follows whatever the operator configured the BN to listen on
// rather than a value baked into solo-weaver; it falls back to
// DefaultBlockNodeHealthPort (which solo-weaver also templates into its base
// values as the explicit default) when the file omits it or none is supplied.
//
// Only `blockNode.ports.health` is consulted — not `blockNode.config.SERVER_PORT`,
// which is the main gRPC/service port (a different port from the /healthz +
// statusz endpoint); using it as a fallback would point bn-mgmt at the wrong port
// whenever an operator overrode SERVER_PORT but left the health port at its chart
// default.
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
