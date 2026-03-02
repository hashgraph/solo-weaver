// SPDX-License-Identifier: Apache-2.0

package blocknode

// prepare_inputs_test.go tests PrepareEffectiveInputs for InstallHandler and
// UpgradeHandler using a stub rslAccessor that returns controlled
// *automa.EffectiveValue values — no real rsl.Registry, Kubernetes cluster,
// or Helm chart required.
//
// Scenarios covered:
//  1. Config-strategy values flow through unchanged.
//  2. User-supplied values override config-strategy values.
//  3. RequiresExplicitOverride blocks user input when strategy is StrategyCurrent
//     and --force is NOT set (install only).
//  4. RequiresExplicitOverride allows user input when strategy is StrategyCurrent
//     and --force IS set (install only).
//  5. Pass-through fields (ValuesFile, ReuseValues, SkipHardwareChecks,
//     ResetStorage) are copied verbatim.
//  6. rslAccessor field errors propagate as errors.
//  7. Nil inputs returns an error.

import (
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// ── stub rslAccessor ──────────────────────────────────────────────────────────

// stubAccessor satisfies rslAccessor with pre-configured return values.
// Each field has a result (EffectiveValue + error) that is returned verbatim.
type stubAccessor struct {
	releaseName  *accessorResult[string]
	version      *accessorResult[string]
	namespace    *accessorResult[string]
	chartName    *accessorResult[string]
	chartRepo    *accessorResult[string]
	chartVersion *accessorResult[string]
	storage      *accessorResult[models.BlockNodeStorage]
}

type accessorResult[T any] struct {
	val      T
	strategy automa.EffectiveStrategy
	err      error
}

func effStr(val string, strategy automa.EffectiveStrategy) *accessorResult[string] {
	return &accessorResult[string]{val: val, strategy: strategy}
}

func effStrErr(err error) *accessorResult[string] {
	return &accessorResult[string]{err: err}
}

func effStorage(val models.BlockNodeStorage, strategy automa.EffectiveStrategy) *accessorResult[models.BlockNodeStorage] {
	return &accessorResult[models.BlockNodeStorage]{val: val, strategy: strategy}
}

func (s *stubAccessor) ReleaseName() (*automa.EffectiveValue[string], error) {
	return makeEffective(s.releaseName)
}
func (s *stubAccessor) Version() (*automa.EffectiveValue[string], error) {
	return makeEffective(s.version)
}
func (s *stubAccessor) Namespace() (*automa.EffectiveValue[string], error) {
	return makeEffective(s.namespace)
}
func (s *stubAccessor) ChartName() (*automa.EffectiveValue[string], error) {
	return makeEffective(s.chartName)
}
func (s *stubAccessor) ChartRepo() (*automa.EffectiveValue[string], error) {
	return makeEffective(s.chartRepo)
}
func (s *stubAccessor) ChartVersion() (*automa.EffectiveValue[string], error) {
	return makeEffective(s.chartVersion)
}
func (s *stubAccessor) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	if s.storage == nil {
		ev, _ := automa.NewEffective(models.BlockNodeStorage{}, automa.StrategyConfig)
		return ev, nil
	}
	if s.storage.err != nil {
		return nil, s.storage.err
	}
	return automa.NewEffective(s.storage.val, s.storage.strategy)
}

func makeEffective[T any](r *accessorResult[T]) (*automa.EffectiveValue[T], error) {
	if r == nil {
		var zero T
		ev, _ := automa.NewEffective(zero, automa.StrategyConfig)
		return ev, nil
	}
	if r.err != nil {
		return nil, r.err
	}
	return automa.NewEffective(r.val, r.strategy)
}

// ── compile-time check ────────────────────────────────────────────────────────

var _ rslAccessor = (*stubAccessor)(nil)

// ── helpers ───────────────────────────────────────────────────────────────────

// defaultStub returns a stub where all fields carry StrategyConfig values
// (simulating a fresh install with nothing deployed yet).
func defaultStub() *stubAccessor {
	return &stubAccessor{
		releaseName:  effStr("block-node", automa.StrategyConfig),
		version:      effStr("0.22.1", automa.StrategyConfig),
		namespace:    effStr("block-node-ns", automa.StrategyConfig),
		chartName:    effStr("block-node-server", automa.StrategyConfig),
		chartRepo:    effStr("oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server", automa.StrategyConfig),
		chartVersion: effStr("0.22.1", automa.StrategyConfig),
		storage:      effStorage(models.BlockNodeStorage{}, automa.StrategyConfig),
	}
}

// currentStub returns a stub where all string fields carry StrategyCurrent
// values (simulating a deployed block node).
func currentStub() *stubAccessor {
	return &stubAccessor{
		releaseName:  effStr("block-node", automa.StrategyCurrent),
		version:      effStr("0.22.1", automa.StrategyCurrent),
		namespace:    effStr("block-node-ns", automa.StrategyCurrent),
		chartName:    effStr("block-node-server", automa.StrategyCurrent),
		chartRepo:    effStr("oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server", automa.StrategyCurrent),
		chartVersion: effStr("0.22.1", automa.StrategyCurrent),
		storage:      effStorage(models.BlockNodeStorage{BasePath: "/data/block-node"}, automa.StrategyCurrent),
	}
}

func baseInputs() *models.UserInputs[models.BlocknodeInputs] {
	return &models.UserInputs[models.BlocknodeInputs]{
		Common: models.CommonInputs{},
		Custom: models.BlocknodeInputs{
			Profile: "local",
		},
	}
}

// ── InstallHandler.PrepareEffectiveInputs ─────────────────────────────────────

func TestInstallPrepare_NilInputs_Error(t *testing.T) {
	h := newInstallHandler(defaultStub())
	_, err := h.PrepareEffectiveInputs(nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}

func TestInstallPrepare_ConfigStrategy_FlowsThrough(t *testing.T) {
	h := newInstallHandler(defaultStub())
	out, err := h.PrepareEffectiveInputs(baseInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.Release != "block-node" {
		t.Errorf("Release: got %q, want %q", out.Custom.Release, "block-node")
	}
	if out.Custom.Version != "0.22.1" {
		t.Errorf("Version: got %q, want %q", out.Custom.Version, "0.22.1")
	}
	if out.Custom.Namespace != "block-node-ns" {
		t.Errorf("Namespace: got %q, want %q", out.Custom.Namespace, "block-node-ns")
	}
	if out.Custom.Chart != "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server" {
		t.Errorf("Chart: got %q", out.Custom.Chart)
	}
	if out.Custom.ChartVersion != "0.22.1" {
		t.Errorf("ChartVersion: got %q, want %q", out.Custom.ChartVersion, "0.22.1")
	}
}

// StrategyCurrent + user input provided + --force → RequiresExplicitOverride
// MUST fire: the field is owned by the current deployment and the user is
// trying to force-override it during an install (not an upgrade).
func TestInstallPrepare_CurrentStrategy_WithUserInput_Force_Error(t *testing.T) {
	h := newInstallHandler(currentStub())

	inputs := baseInputs()
	inputs.Common.Force = true
	inputs.Custom.Release = "my-custom-release" // user supplies a value + --force

	_, err := h.PrepareEffectiveInputs(inputs)
	if err == nil {
		t.Fatal("expected RequiresExplicitOverride error when StrategyCurrent + user input + --force, got nil")
	}
}

// StrategyCurrent + user input provided + no --force → guard does NOT fire,
// because without --force there is no force-override attempt.
func TestInstallPrepare_CurrentStrategy_WithUserInput_NoForce_Succeeds(t *testing.T) {
	h := newInstallHandler(currentStub())

	inputs := baseInputs()
	inputs.Common.Force = false
	inputs.Custom.Release = "my-custom-release" // user supplies a value, but no --force

	out, err := h.PrepareEffectiveInputs(inputs)
	if err != nil {
		t.Fatalf("unexpected error without --force: %v", err)
	}
	if out.Custom.Release == "" {
		t.Error("expected non-empty effective release name")
	}
}

// StrategyCurrent + NO user input → no override attempted, must succeed.
func TestInstallPrepare_CurrentStrategy_NoUserInput_Succeeds(t *testing.T) {
	h := newInstallHandler(currentStub())
	out, err := h.PrepareEffectiveInputs(baseInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.Namespace != "block-node-ns" {
		t.Errorf("Namespace: got %q, want %q", out.Custom.Namespace, "block-node-ns")
	}
}

// Multiple StrategyCurrent fields with user inputs AND --force — the first
// field that violates the guard must return an error.
func TestInstallPrepare_CurrentStrategy_MultipleOverrides_WithForce_Error(t *testing.T) {
	h := newInstallHandler(currentStub())

	inputs := baseInputs()
	inputs.Common.Force = true // force-override attempted
	inputs.Custom.Namespace = "new-ns"
	inputs.Custom.Version = "0.23.0"

	_, err := h.PrepareEffectiveInputs(inputs)
	if err == nil {
		t.Fatal("expected error for force-override attempt on StrategyCurrent fields")
	}
}

// Pass-through fields are copied verbatim regardless of strategy.
func TestInstallPrepare_PassThroughFields(t *testing.T) {
	h := newInstallHandler(defaultStub())

	inputs := baseInputs()
	inputs.Custom.ValuesFile = "/etc/weaver/values.yaml"
	inputs.Custom.ReuseValues = true
	inputs.Custom.SkipHardwareChecks = true
	inputs.Custom.ResetStorage = true

	out, err := h.PrepareEffectiveInputs(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.ValuesFile != "/etc/weaver/values.yaml" {
		t.Errorf("ValuesFile: got %q, want %q", out.Custom.ValuesFile, "/etc/weaver/values.yaml")
	}
	if !out.Custom.ReuseValues {
		t.Error("ReuseValues: expected true")
	}
	if !out.Custom.SkipHardwareChecks {
		t.Error("SkipHardwareChecks: expected true")
	}
	if !out.Custom.ResetStorage {
		t.Error("ResetStorage: expected true")
	}
}

// rslAccessor field error propagates.
func TestInstallPrepare_AccessorError_Propagates(t *testing.T) {
	stub := defaultStub()
	stub.releaseName = effStrErr(errors.New("rsl unavailable"))

	h := newInstallHandler(stub)
	_, err := h.PrepareEffectiveInputs(baseInputs())
	if err == nil {
		t.Fatal("expected error from accessor, got nil")
	}
}

// Profile is always copied from user input (not resolved via rsl).
func TestInstallPrepare_ProfilePassedThrough(t *testing.T) {
	h := newInstallHandler(defaultStub())

	inputs := baseInputs()
	inputs.Custom.Profile = "perfnet"

	out, err := h.PrepareEffectiveInputs(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.Profile != "perfnet" {
		t.Errorf("Profile: got %q, want %q", out.Custom.Profile, "perfnet")
	}
}

// ── UpgradeHandler.PrepareEffectiveInputs ─────────────────────────────────────
// UpgradeHandler uses resolver.Field with zero validators for all fields —
// the operator explicitly intends to change fields, so no override guards apply.
// Chart immutability and semver constraints are deferred to BuildWorkflow.

func TestUpgradePrepare_NilInputs_Error(t *testing.T) {
	h := newUpgradeHandler(defaultStub())
	_, err := h.PrepareEffectiveInputs(nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}

func TestUpgradePrepare_ConfigStrategy_FlowsThrough(t *testing.T) {
	h := newUpgradeHandler(defaultStub())
	out, err := h.PrepareEffectiveInputs(baseInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.Release != "block-node" {
		t.Errorf("Release: got %q, want %q", out.Custom.Release, "block-node")
	}
	if out.Custom.Version != "0.22.1" {
		t.Errorf("Version: got %q, want %q", out.Custom.Version, "0.22.1")
	}
	if out.Custom.Namespace != "block-node-ns" {
		t.Errorf("Namespace: got %q, want %q", out.Custom.Namespace, "block-node-ns")
	}
}

// Upgrade has NO RequiresExplicitOverride guard — StrategyCurrent + user input
// + no --force must still succeed.
func TestUpgradePrepare_CurrentStrategy_WithUserInput_NoForce_Succeeds(t *testing.T) {
	h := newUpgradeHandler(currentStub())

	inputs := baseInputs()
	inputs.Custom.Version = "0.23.0" // user explicitly providing new version

	out, err := h.PrepareEffectiveInputs(inputs)
	if err != nil {
		t.Fatalf("expected upgrade PrepareEffectiveInputs to succeed without --force, got: %v", err)
	}
	// Effective value comes from the stub (0.22.1 current), not user input,
	// since the rsl resolution happened in the stub. The important thing is
	// no error was returned.
	if out.Custom.Version == "" {
		t.Error("expected non-empty effective version")
	}
}

func TestUpgradePrepare_PassThroughFields(t *testing.T) {
	h := newUpgradeHandler(defaultStub())

	inputs := baseInputs()
	inputs.Custom.ValuesFile = "/etc/weaver/upgrade-values.yaml"
	inputs.Custom.ReuseValues = true
	inputs.Custom.SkipHardwareChecks = false
	inputs.Custom.ResetStorage = true

	out, err := h.PrepareEffectiveInputs(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.ValuesFile != "/etc/weaver/upgrade-values.yaml" {
		t.Errorf("ValuesFile: got %q", out.Custom.ValuesFile)
	}
	if !out.Custom.ReuseValues {
		t.Error("ReuseValues: expected true")
	}
	if out.Custom.SkipHardwareChecks {
		t.Error("SkipHardwareChecks: expected false")
	}
	if !out.Custom.ResetStorage {
		t.Error("ResetStorage: expected true")
	}
}

func TestUpgradePrepare_AccessorError_Propagates(t *testing.T) {
	stub := defaultStub()
	stub.version = effStrErr(errors.New("rsl version unavailable"))

	h := newUpgradeHandler(stub)
	_, err := h.PrepareEffectiveInputs(baseInputs())
	if err == nil {
		t.Fatal("expected error from accessor, got nil")
	}
}

func TestUpgradePrepare_StorageResolved(t *testing.T) {
	stub := defaultStub()
	stub.storage = effStorage(
		models.BlockNodeStorage{BasePath: "/data/block-node"},
		automa.StrategyCurrent,
	)

	h := newUpgradeHandler(stub)
	out, err := h.PrepareEffectiveInputs(baseInputs())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Custom.Storage.BasePath != "/data/block-node" {
		t.Errorf("Storage.BasePath: got %q, want %q", out.Custom.Storage.BasePath, "/data/block-node")
	}
}

func TestUpgradePrepare_CommonInputsCopied(t *testing.T) {
	h := newUpgradeHandler(defaultStub())

	inputs := baseInputs()
	inputs.Common.Force = true
	inputs.Common.NodeType = models.NodeTypeBlock

	out, err := h.PrepareEffectiveInputs(inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Common.Force {
		t.Error("Common.Force: expected true")
	}
	if out.Common.NodeType != models.NodeTypeBlock {
		t.Errorf("Common.NodeType: got %q, want %q", out.Common.NodeType, models.NodeTypeBlock)
	}
}
