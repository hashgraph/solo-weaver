// SPDX-License-Identifier: Apache-2.0

package resolver_test

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/resolver"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// mustVal wraps automa.NewValue and fails the test on error.
func mustVal[T any](t *testing.T, v T) automa.Value[T] {
	t.Helper()
	val, err := automa.NewValue(v)
	require.NoError(t, err)
	return val
}

// val extracts the concrete T from an EffectiveValue — ev.Get().Val().
func val[T any](ev *automa.EffectiveValue[T]) T {
	return ev.Get().Val()
}

// ── WithFunc ────────────────────────────────────────────────────────────────

func TestWithFunc_NotDeployed_NoUserInput_UsesDefault(t *testing.T) {
	def := mustVal(t, "default-ns")

	ev, err := resolver.WithFunc[string](def, nil, "current-ns",
		func() bool { return false }, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, "default-ns", val(ev))
	assert.Equal(t, automa.StrategyConfig, ev.Strategy())
}

func TestWithFunc_NotDeployed_WithUserInput_UsesUserInput(t *testing.T) {
	def := mustVal(t, "default-ns")
	user := mustVal(t, "user-ns")

	ev, err := resolver.WithFunc[string](def, user, "current-ns",
		func() bool { return false }, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, "user-ns", val(ev))
	assert.Equal(t, automa.StrategyUserInput, ev.Strategy())
}

func TestWithFunc_Deployed_UsesCurrent(t *testing.T) {
	def := mustVal(t, "default-ns")
	user := mustVal(t, "user-ns")

	ev, err := resolver.WithFunc[string](def, user, "deployed-ns",
		func() bool { return true }, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, "deployed-ns", val(ev))
	assert.Equal(t, automa.StrategyCurrent, ev.Strategy())
}

func TestWithFunc_NotDeployed_EmptyUserInput_FallsBackToDefault(t *testing.T) {
	def := mustVal(t, "default-ns")
	user := mustVal(t, "") // empty string — treated as not provided

	ev, err := resolver.WithFunc[string](def, user, "current-ns",
		func() bool { return false }, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, "default-ns", val(ev))
	assert.Equal(t, automa.StrategyConfig, ev.Strategy())
}

func TestWithFunc_CustomIsEmpty_RespectsCustomCheck(t *testing.T) {
	def := mustVal(t, "default-ns")
	// user supplies "IGNORE" which our custom isEmpty treats as empty
	user := mustVal(t, "IGNORE")

	ev, err := resolver.WithFunc[string](def, user, "",
		func() bool { return false },
		nil,
		func(v string) bool { return v == "IGNORE" },
	)

	require.NoError(t, err)
	assert.Equal(t, "default-ns", val(ev))
	assert.Equal(t, automa.StrategyConfig, ev.Strategy())
}

func TestWithFunc_Deployed_OverridesUserInput(t *testing.T) {
	// Even when user provides a value, deployed state wins.
	def := mustVal(t, "default-ns")
	user := mustVal(t, "user-ns")

	ev, err := resolver.WithFunc[string](def, user, "live-ns",
		func() bool { return true }, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, "live-ns", val(ev))
	assert.Equal(t, automa.StrategyCurrent, ev.Strategy())
}

// ── ForStatus ───────────────────────────────────────────────────────────────

func TestForStatus_DeployedStatus_ReturnsCurrent(t *testing.T) {
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")

	ev, cache, err := resolver.ForStatus[string](def, user, "v0.9.0",
		release.StatusDeployed, true)

	require.NoError(t, err)
	assert.Equal(t, "v0.9.0", val(ev))
	assert.Equal(t, automa.StrategyCurrent, ev.Strategy())
	assert.True(t, cache, "cacheResult should be forwarded unchanged")
}

func TestForStatus_UndeployedStatus_WithUserInput_UsesUserInput(t *testing.T) {
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")

	ev, cache, err := resolver.ForStatus[string](def, user, "",
		release.StatusUnknown, false)

	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", val(ev))
	assert.Equal(t, automa.StrategyUserInput, ev.Strategy())
	assert.False(t, cache, "cacheResult false should be forwarded unchanged")
}

func TestForStatus_UndeployedStatus_NoUserInput_UsesDefault(t *testing.T) {
	def := mustVal(t, "v1.0.0")

	ev, _, err := resolver.ForStatus[string](def, nil, "",
		release.StatusUnknown, true)

	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", val(ev))
	assert.Equal(t, automa.StrategyConfig, ev.Strategy())
}

func TestForStatus_SupersedesStatus_TreatedAsNotDeployed(t *testing.T) {
	// release.StatusSuperseded is not "deployed" — user input should win.
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v3.0.0")

	ev, _, err := resolver.ForStatus[string](def, user, "v0.5.0",
		release.StatusSuperseded, true)

	require.NoError(t, err)
	assert.Equal(t, "v3.0.0", val(ev))
	assert.Equal(t, automa.StrategyUserInput, ev.Strategy())
}

// ── Struct type (non-string) ─────────────────────────────────────────────────

type storageStub struct {
	Size string
	Path string
}

func (s storageStub) isEmpty() bool { return s.Size == "" && s.Path == "" }

func TestWithFunc_Struct_NotDeployed_WithUserInput(t *testing.T) {
	def := mustVal(t, storageStub{Size: "5Gi", Path: "/default"})
	user := mustVal(t, storageStub{Size: "10Gi", Path: "/user"})

	ev, err := resolver.WithFunc[storageStub](
		def, user, storageStub{},
		func() bool { return false },
		nil,
		func(v storageStub) bool { return v.isEmpty() },
	)

	require.NoError(t, err)
	assert.Equal(t, "10Gi", val(ev).Size)
	assert.Equal(t, automa.StrategyUserInput, ev.Strategy())
}

func TestWithFunc_Struct_Deployed_ReturnsCurrent(t *testing.T) {
	def := mustVal(t, storageStub{Size: "5Gi", Path: "/default"})
	user := mustVal(t, storageStub{Size: "10Gi", Path: "/user"})
	current := storageStub{Size: "20Gi", Path: "/live"}

	ev, err := resolver.WithFunc[storageStub](
		def, user, current,
		func() bool { return true },
		nil,
		func(v storageStub) bool { return v.isEmpty() },
	)

	require.NoError(t, err)
	assert.Equal(t, "20Gi", val(ev).Size)
	assert.Equal(t, automa.StrategyCurrent, ev.Strategy())
}

// ── ImmutableOnDeploy ────────────────────────────────────────────────────────

func TestImmutableOnDeploy_CurrentStrategy_Allowed(t *testing.T) {
	// Value came from current state — no change, must pass.
	def := mustVal(t, "prod-ns")
	user := mustVal(t, "new-ns")

	ev, err := resolver.WithFunc[string](def, user, "prod-ns",
		func() bool { return true }, nil, nil)
	require.NoError(t, err)
	require.Equal(t, automa.StrategyCurrent, ev.Strategy())

	validate := resolver.ImmutableOnDeploy[string]("namespace")
	assert.NoError(t, validate(ev))
}

func TestImmutableOnDeploy_UserInputStrategy_Rejected(t *testing.T) {
	// User supplied input while not deployed — strategy = UserInput.
	// When subsequently the resource IS deployed and a second install is
	// attempted with user input, the validator must reject it.
	def := mustVal(t, "prod-ns")
	user := mustVal(t, "new-ns")

	ev, err := resolver.WithFunc[string](def, user, "prod-ns",
		func() bool { return false }, nil, nil)
	require.NoError(t, err)
	require.Equal(t, automa.StrategyUserInput, ev.Strategy())

	validate := resolver.ImmutableOnDeploy[string]("namespace")
	err = validate(ev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
	assert.Contains(t, err.Error(), "immutable")
}

func TestImmutableOnDeploy_ConfigStrategy_Allowed(t *testing.T) {
	// No user input, not deployed — strategy = Config. Must pass.
	def := mustVal(t, "prod-ns")

	ev, err := resolver.WithFunc[string](def, nil, "",
		func() bool { return false }, nil, nil)
	require.NoError(t, err)
	require.Equal(t, automa.StrategyConfig, ev.Strategy())

	validate := resolver.ImmutableOnDeploy[string]("namespace")
	assert.NoError(t, validate(ev))
}

// ── Field ────────────────────────────────────────────────────────────────────

func TestField_NoValidators_ReturnsSelectedValue(t *testing.T) {
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")

	ev, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) {
			return resolver.WithFunc[string](def, user, "", func() bool { return false }, nil, nil)
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "v2.0.0", val(ev))
	assert.Equal(t, automa.StrategyUserInput, ev.Strategy())
}

func TestField_ValidatorPasses_ReturnsValue(t *testing.T) {
	def := mustVal(t, "prod-ns")

	ev, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) {
			// not deployed, no user input → StrategyConfig
			return resolver.WithFunc[string](def, nil, "", func() bool { return false }, nil, nil)
		},
		resolver.ImmutableOnDeploy[string]("namespace"),
	)

	require.NoError(t, err)
	assert.Equal(t, "prod-ns", val(ev))
}

func TestField_ValidatorFails_ReturnsError(t *testing.T) {
	def := mustVal(t, "prod-ns")
	user := mustVal(t, "new-ns")

	_, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) {
			// not deployed, user input present → StrategyUserInput
			return resolver.WithFunc[string](def, user, "", func() bool { return false }, nil, nil)
		},
		resolver.ImmutableOnDeploy[string]("namespace"),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")
}

func TestField_MultipleValidators_AllRun(t *testing.T) {
	// Two validators — first passes, second fails — error must surface.
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")

	alwaysPass := resolver.Validator[string](func(_ *automa.EffectiveValue[string]) error {
		return nil
	})
	alwaysFail := resolver.Validator[string](func(_ *automa.EffectiveValue[string]) error {
		return errorx.IllegalArgument.New("second validator failed")
	})

	_, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) {
			return resolver.WithFunc[string](def, user, "", func() bool { return false }, nil, nil)
		},
		alwaysPass,
		alwaysFail,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "second validator failed")
}

func TestField_SelectionError_PropagatesBeforeValidation(t *testing.T) {
	// A nil defaultVal causes automa.NewEffective to error — validator must never run.
	called := false
	neverCalled := resolver.Validator[string](func(_ *automa.EffectiveValue[string]) error {
		called = true
		return nil
	})

	_, err := resolver.Field(
		func() (*automa.EffectiveValue[string], error) {
			return nil, errorx.IllegalArgument.New("selection failed")
		},
		neverCalled,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "selection failed")
	assert.False(t, called, "validator must not run when selection errors")
}

// ── RequiresExplicitOverride ──────────────────────────────────────────────────

func TestRequiresExplicitOverride_NoInput_NeverFires(t *testing.T) {
	// User did not supply a value — guard must not fire regardless of force.
	def := mustVal(t, "v1.0.0")
	ev, err := resolver.WithFunc[string](def, nil, "v1.0.0",
		func() bool { return true }, nil, nil)
	require.NoError(t, err)

	validate := resolver.RequiresExplicitOverride[string]("version", false, true, "use upgrade")
	assert.NoError(t, validate(ev))
}

func TestRequiresExplicitOverride_HasInput_StrategyUserInput_NeverFires(t *testing.T) {
	// User input won the selection — this is the intended override path, must pass.
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")
	ev, err := resolver.WithFunc[string](def, user, "",
		func() bool { return false }, nil, nil)
	require.NoError(t, err)
	require.Equal(t, automa.StrategyUserInput, ev.Strategy())

	validate := resolver.RequiresExplicitOverride[string]("version", true, true, "use upgrade")
	assert.NoError(t, validate(ev))
}

func TestRequiresExplicitOverride_HasInput_CurrentWon_Force_Fires(t *testing.T) {
	// Current state won (deployed), user also provided input, force=true → reject.
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")
	ev, err := resolver.WithFunc[string](def, user, "v0.9.0",
		func() bool { return true }, nil, nil)
	require.NoError(t, err)
	require.Equal(t, automa.StrategyCurrent, ev.Strategy())

	validate := resolver.RequiresExplicitOverride[string]("version", true, true, "use upgrade")
	err = validate(ev)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
	assert.Contains(t, err.Error(), "cannot override")
}

func TestRequiresExplicitOverride_HasInput_CurrentWon_NoForce_NeverFires(t *testing.T) {
	// Current state won but force=false — guard must stay silent.
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")
	ev, err := resolver.WithFunc[string](def, user, "v0.9.0",
		func() bool { return true }, nil, nil)
	require.NoError(t, err)

	validate := resolver.RequiresExplicitOverride[string]("version", true, false, "use upgrade")
	assert.NoError(t, validate(ev))
}

// ── OverrideHint ──────────────────────────────────────────────────────────────

func TestOverrideHint_Present(t *testing.T) {
	def := mustVal(t, "v1.0.0")
	user := mustVal(t, "v2.0.0")
	ev, err := resolver.WithFunc[string](def, user, "v0.9.0",
		func() bool { return true }, nil, nil)
	require.NoError(t, err)

	validate := resolver.RequiresExplicitOverride[string]("version", true, true, "use upgrade")
	verr := validate(ev)
	require.Error(t, verr)

	hint, ok := resolver.OverrideHint(verr)
	assert.True(t, ok)
	assert.Equal(t, "use upgrade", hint)
}

func TestOverrideHint_Absent(t *testing.T) {
	plain := errorx.IllegalArgument.New("no hint attached")
	_, ok := resolver.OverrideHint(plain)
	assert.False(t, ok)
}
