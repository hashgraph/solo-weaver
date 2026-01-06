// SPDX-License-Identifier: Apache-2.0

package runtime

import (
	"reflect"

	"github.com/automa-saga/automa"
	"helm.sh/helm/v3/pkg/release"
)

// resolveEffectiveWithFunc computes the effective value and strategy for a field.
// - defaultVal, userInput: automa.Value[T] provided by the runtime.
// - currentPresent: true when the resource is deployed (i.e. current should be considered).
// - currentVal: current value from the cluster/state.
// - equal: optional equality function; if nil reflect.DeepEqual is used.
// - isEmpty: optional emptiness check; if nil reflect.Value.IsZero is used.
// Returns (*automa.EffectiveValue[T], usedUserInput bool, error).
func resolveEffectiveWithFunc[T any](
	defaultVal automa.Value[T],
	userInput automa.Value[T],
	currentVal T,
	isDeployed func() bool,
	equal func(a, b T) bool,
	isEmpty func(v T) bool,
) (*automa.EffectiveValue[T], error) {
	if equal == nil {
		equal = func(a, b T) bool { return reflect.DeepEqual(a, b) }
	}
	if isEmpty == nil {
		isEmpty = func(v T) bool { return reflect.ValueOf(v).IsZero() }
	}

	val := defaultVal.Val()
	strategy := automa.StrategyConfig

	if userInput != nil && !isEmpty(userInput.Val()) {
		// if deployed and current differs from user input -> keep current
		if isDeployed() && !equal(currentVal, userInput.Val()) {
			val = currentVal
			strategy = automa.StrategyCurrent
		} else {
			val = userInput.Val()
			strategy = automa.StrategyUserInput
		}
	}

	ev, err := automa.NewEffective(val, strategy)
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// resolveEffective computes the effective value and strategy for a field.
// It is a wrapper over resolveEffectiveWithFunc with default equality and emptiness checks.
// - defaultVal, userInput: automa.Value[T] provided by the runtime.
// - currentVal: current value from the cluster/state.
// - status: release.Status to determine if deployed.
// - cacheResult: whether the result can be cached. It is returned back to the caller.
// Returns (*automa.EffectiveValue[T], error).
func resolveEffective[T any](
	defaultVal automa.Value[T],
	userInput automa.Value[T],
	currentVal T,
	status release.Status,
	cacheResult bool,
) (*automa.EffectiveValue[T], bool, error) {
	isDeployed := func() bool {
		return status == release.StatusDeployed
	}

	ev, err := resolveEffectiveWithFunc[T](defaultVal, userInput, currentVal, isDeployed, nil, nil)
	if err != nil {
		return nil, cacheResult, err
	}

	return ev, cacheResult, nil
}
