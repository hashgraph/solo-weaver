// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/logx"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
)

// patchBlockNodeState persists fields that cannot be recovered from the Helm
// release or PersistentVolumes into the runtime state before flushing to disk.
//
//   - ChartRef: the OCI / repo reference is not stored in Helm release metadata.
//   - Storage.BasePath: a weaver concept; Kubernetes only knows the individual PV
//     hostPaths that the reality checker reads back. Without this patch, BasePath
//     is lost after every FlushState → Refresh cycle, and the next interactive
//     prompt would re-fill the PV-derived individual paths instead of the
//     operator-chosen base path.
//
// Profile persistence is handled centrally by BaseHandler.FlushState via ProfileExtractor.
func patchBlockNodeState() func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
	return func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
		if st.BlockNodeState.ReleaseInfo.Status != release.StatusDeployed {
			return nil
		}
		if effectiveInputs.Custom.Chart != "" {
			logx.As().Debug().Str("chartRef", effectiveInputs.Custom.Chart).
				Msg("Persisted block node chart ref into runtime state")
			st.BlockNodeState.ReleaseInfo.ChartRef = effectiveInputs.Custom.Chart
		}
		if effectiveInputs.Custom.Storage.BasePath != "" {
			logx.As().Debug().Str("basePath", effectiveInputs.Custom.Storage.BasePath).
				Msg("Persisted block node storage base path into runtime state")
			st.BlockNodeState.Storage.BasePath = effectiveInputs.Custom.Storage.BasePath
		}
		if effectiveInputs.Custom.PluginPreset != "" {
			logx.As().Debug().Str("pluginPreset", effectiveInputs.Custom.PluginPreset).
				Msg("Persisted block node plugin preset into runtime state")
			st.BlockNodeState.PluginPreset = effectiveInputs.Custom.PluginPreset
		}
		if effectiveInputs.Custom.PluginList != "" {
			logx.As().Debug().Str("pluginList", effectiveInputs.Custom.PluginList).
				Msg("Persisted block node plugin list into runtime state")
			st.BlockNodeState.PluginList = effectiveInputs.Custom.PluginList
		}
		return nil
	}
}

// patchBlockNodeStateWithTrafficShaping extends patchBlockNodeState with the
// TrafficShapingDisabled field, persisting the caller's resolved
// TrafficShapingEnabled decision. It is used by the two commands that resolve a
// real traffic-shaping target: `block node install` and `block node reconfigure`.
//
// It must NOT be used by upgrade/reset/uninstall — those do not resolve
// TrafficShapingEnabled, so their effectiveInputs.Custom value is the unset zero
// value and would silently flip a true decision back to "disabled" on every run.
// Upgrade in particular reads the persisted decision (via
// currentState.BlockNodeState) to drive its silent convergence and therefore keeps
// the plain patchBlockNodeState, which leaves this field untouched.
func patchBlockNodeStateWithTrafficShaping() func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
	base := patchBlockNodeState()
	return func(st *state.State, effectiveInputs models.UserInputs[models.BlockNodeInputs]) error {
		st.BlockNodeState.TrafficShapingDisabled = !effectiveInputs.Custom.TrafficShapingEnabled
		logx.As().Debug().Bool("trafficShapingDisabled", st.BlockNodeState.TrafficShapingDisabled).
			Msg("Persisted block node traffic-shaping decision into runtime state")

		// Persist the host-firewall decision + content (host-scoped) so a later
		// reconfigure/upgrade can re-assert the inet weaver-host-firewall table without the operator
		// re-supplying the allowlist. Written even when the block node is not (yet)
		// deployed, mirroring TrafficShapingDisabled — the firewall is host-scoped and
		// its application is independent of the Helm release status.
		patchMachineFirewallFromConfig(st)

		// Persist the traffic-shaping content only when it was enabled and a real
		// target was resolved, so upgrade/reconfigure re-assert the same NIC/rate/
		// overrides instead of auto-detecting. When disabled we leave any prior
		// Shaping record intact (the decision on TrafficShapingDisabled governs re-assert).
		if effectiveInputs.Custom.TrafficShapingEnabled {
			patchBlockNodeShaping(st, effectiveInputs.Custom)
		}
		return base(st, effectiveInputs)
	}
}

// patchMachineFirewallFromConfig records the resolved host-firewall configuration
// (config.Get().Host, set by common.ResolveHostFirewallConfig earlier in the CLI
// flow) into MachineState.Firewall. The enable/disable decision is always kept in
// sync; the CIDR/port content is updated only when the firewall is enabled with a
// non-empty allowlist, so a disable — or an enable with an empty allowlist, which
// the NetworkFirewallCreate step skips — flips Disabled but preserves the
// last-known-good allowlist for a future bare re-enable.
func patchMachineFirewallFromConfig(st *state.State) {
	hostCfg := config.Get().Host

	fw := st.MachineState.Firewall
	if fw == nil {
		fw = &state.HostFirewallState{}
	}
	fw.Disabled = hostCfg.Disabled

	if !hostCfg.Disabled && len(hostCfg.ManagementCIDRs) > 0 {
		fw.ManagementCIDRs = hostCfg.ManagementCIDRs
		fw.BlockedCIDRs = hostCfg.BlockedCIDRs
		fw.MgmtPorts = hostCfg.MgmtPorts
		fw.PodCIDR = hostCfg.PodCIDR
		fw.InClusterPorts = hostCfg.InClusterPorts
	}

	st.MachineState.Firewall = fw
	logx.As().Debug().
		Bool("disabled", fw.Disabled).
		Int("managementCidrs", len(fw.ManagementCIDRs)).
		Msg("Persisted host-firewall configuration into runtime state")
}

// patchBlockNodeShaping records the resolved traffic-shaping content bundle into
// BlockNodeState.Shaping so upgrade/reconfigure can re-assert the operator's
// original egress NIC and link rate, and so the last --shape request stays on
// record.
//
// ShapeOverrides is no longer re-asserted as an effective input (#1037), so a
// bare reconfigure arrives here with an empty map. Carry the previously recorded
// request over in that case instead of erasing it: the reality refresh already
// goes out of its way to preserve this record across a state rebuild (see
// reality.blocknode_checker, "cannot be recovered from the Helm release or the
// live cluster"), and a routine reconfigure should not be the thing that drops
// it. Current per-class values live in the shape registry either way.
func patchBlockNodeShaping(st *state.State, ins models.BlockNodeInputs) {
	overrides := ins.ShapeOverrides
	if len(overrides) == 0 && st.BlockNodeState.Shaping != nil {
		overrides = st.BlockNodeState.Shaping.ShapeOverrides
	}
	st.BlockNodeState.Shaping = &state.ShapingState{
		EgressInterface: ins.EgressInterface,
		LinkRate:        ins.LinkRate,
		ShapeOverrides:  overrides,
	}
	logx.As().Debug().
		Str("egressInterface", ins.EgressInterface).
		Str("linkRate", ins.LinkRate).
		Int("shapeOverrides", len(overrides)).
		Msg("Persisted block node traffic-shaping content into runtime state")
}

// resolveBlocknodeEffectiveInputs resolves common fields for blocknode commands.
func resolveBlocknodeEffectiveInputs(
	runtime *rsl.BlockNodeRuntimeResolver,
	intent models.Intent,
	inputs models.UserInputs[models.BlockNodeInputs],
	validator func(*models.UserInputs[models.BlockNodeInputs]) error,
) (*models.UserInputs[models.BlockNodeInputs], error) {
	// Set user inputs on the runtime state so they can be accessed by resolver strategies.
	runtime.WithIntent(intent).WithUserInputs(inputs.Custom)

	effReleaseName, err := runtime.ReleaseName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node release name: %v", err)
	}
	logx.As().Debug().
		Any("releaseName", effReleaseName).
		Msg("Determined effective block node release name")

	effNamespace, err := runtime.Namespace()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node namespace: %v", err)
	}
	logx.As().Debug().
		Any("namespace", effNamespace).
		Msg("Determined effective block node namespace")

	effChartName, err := runtime.ChartName()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart name: %v", err)
	}
	logx.As().Debug().
		Any("chartName", effChartName).
		Msg("Determined effective block node chart name")

	effChartRepo, err := runtime.ChartRef()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart repo: %v", err)
	}
	logx.As().Debug().
		Any("chartRepo", effChartRepo).
		Msg("Determined effective block node chart repo")

	effChartVersion, err := runtime.ChartVersion()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node chart version: %v", err)
	}
	logx.As().Debug().
		Any("chartVersion", effChartVersion).
		Msg("Determined effective block node chart version")

	effStorage, err := runtime.Storage()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node storage: %v", err)
	}
	logx.As().Debug().
		Any("storage", effStorage).
		Msg("Determined effective block node storage")

	effHistoricRetention, err := runtime.HistoricRetention()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node historic retention: %v", err)
	}
	logx.As().Debug().
		Any("historicRetention", effHistoricRetention).
		Msg("Determined effective block node historic retention")

	effRecentRetention, err := runtime.RecentRetention()
	if err != nil {
		return nil, errorx.IllegalState.New("failed to resolve block node recent retention: %v", err)
	}
	logx.As().Debug().
		Any("recentRetention", effRecentRetention).
		Msg("Determined effective block node recent retention")

	// Traffic-shaping content (egress NIC, link rate, per-class overrides) has no
	// resolver tier — it is passed through from user input. Fall back to the
	// persisted BlockNodeState.Shaping for the NIC and the link rate when the
	// operator did not supply them, so `upgrade` (which never prompts for these)
	// and a bare `reconfigure` re-assert the operator's original egress device and
	// trunk rate instead of auto-detecting. Explicit user input always wins. On a
	// fresh install CurrentState carries no Shaping, so this is a no-op there.
	//
	// ShapeOverrides is deliberately NOT backfilled: it records what --shape asked
	// for at install time, and the shape registry already holds the result. Re-
	// asserting it here would re-apply a stale install-time value on top of a later
	// `network shape set` on the same class — the clobber this fallback was meant
	// to help avoid (#1037). Per-class values now come from the registry, which the
	// tc steps preserve across a re-provision at an unchanged trunk rate. The state
	// record itself is still kept lossless — see patchBlockNodeShaping, which
	// carries the previous request over when this run supplied none.
	egressInterface := inputs.Custom.EgressInterface
	linkRate := inputs.Custom.LinkRate
	if current, err := runtime.CurrentState(); err == nil && current.Shaping != nil {
		if egressInterface == "" {
			egressInterface = current.Shaping.EgressInterface
		}
		if linkRate == "" {
			linkRate = current.Shaping.LinkRate
		}
		logx.As().Debug().
			Str("egressInterface", egressInterface).
			Str("linkRate", linkRate).
			Msg("Applied persisted traffic-shaping content as fallback for unset inputs")
	}

	effectiveInputs := models.UserInputs[models.BlockNodeInputs]{
		Common: inputs.Common,
		Custom: models.BlockNodeInputs{
			// Resolved via resolver
			Profile:           inputs.Custom.Profile,
			Release:           effReleaseName.Get().Val(),
			Namespace:         effNamespace.Get().Val(),
			ChartName:         effChartName.Get().Val(),
			Chart:             effChartRepo.Get().Val(),
			ChartVersion:      effChartVersion.Get().Val(),
			Storage:           effStorage.Get().Val(),
			HistoricRetention: effHistoricRetention.Get().Val(),
			RecentRetention:   effRecentRetention.Get().Val(),
			// Passed through from user input (no resolution)
			ValuesFile:            inputs.Custom.ValuesFile,
			ReuseValues:           inputs.Custom.ReuseValues,
			SkipHardwareChecks:    inputs.Custom.SkipHardwareChecks,
			ResetStorage:          inputs.Custom.ResetStorage,
			PurgeStorage:          inputs.Custom.PurgeStorage,
			NoRestart:             inputs.Custom.NoRestart,
			LoadBalancerEnabled:   inputs.Custom.LoadBalancerEnabled,
			PluginPreset:          inputs.Custom.PluginPreset,
			PluginList:            inputs.Custom.PluginList,
			EgressInterface:       egressInterface,
			LinkRate:              linkRate,
			ShapeOverrides:        inputs.Custom.ShapeOverrides,
			TrafficShapingEnabled: inputs.Custom.TrafficShapingEnabled,
			Timeout:               inputs.Custom.Timeout,
			StatuszBaseURL:        inputs.Custom.StatuszBaseURL,
			StatuszPollInterval:   inputs.Custom.StatuszPollInterval,
		},
	}

	if validator != nil {
		if err := validator(&effectiveInputs); err != nil {
			return nil, err
		}
	}

	logx.As().Debug().Any("effectiveUserInputs", effectiveInputs).
		Msg("Determined effective user inputs for block node")

	return &effectiveInputs, nil
}

// storagePathsChanged returns true when the requested storage configuration differs
// from the currently deployed one. Both sides go through ResolveStoragePaths so
// base-path expansion and sanitization are applied consistently before comparing.
// This is pure path math — no Manager / cluster client is constructed.
func storagePathsChanged(deployed models.BlockNodeStorage, requested models.BlockNodeInputs) (bool, error) {
	dArchive, dLive, dLog, dOpt, err := bnpkg.ResolveStoragePaths(deployed, requested.ChartVersion)
	if err != nil {
		return false, err
	}

	rArchive, rLive, rLog, rOpt, err := bnpkg.ResolveStoragePaths(requested.Storage, requested.ChartVersion)
	if err != nil {
		return false, err
	}

	if dArchive != rArchive || dLive != rLive || dLog != rLog {
		return true, nil
	}
	for i := range rOpt {
		if i >= len(dOpt) || dOpt[i] != rOpt[i] {
			return true, nil
		}
	}
	return false, nil
}
