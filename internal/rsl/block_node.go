package rsl

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

var blockNodeRuntimeSingleton *BlockNodeRuntimeState

// BlockNodeRuntimeState is the runtime state of the block-node.
// It is used to determine the effective values of the block-node based on the default (config), current runtime state and provided inputs.
type BlockNodeRuntimeState struct {
	*Base[state.BlockNodeState]
	releaseName  *automa.RuntimeValue[string]
	reality      reality.Checker
	version      *automa.RuntimeValue[string]
	namespace    *automa.RuntimeValue[string]
	chartName    *automa.RuntimeValue[string]
	chartRepo    *automa.RuntimeValue[string]
	chartVersion *automa.RuntimeValue[string]
	storage      *automa.RuntimeValue[models.BlockNodeStorage]
}

func (br *BlockNodeRuntimeState) Namespace() (*automa.EffectiveValue[string], error) {
	return br.namespace.Effective()
}

func (br *BlockNodeRuntimeState) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	return br.storage.Effective()
}

func (br *BlockNodeRuntimeState) Version() (*automa.EffectiveValue[string], error) {
	return br.version.Effective()
}

func (br *BlockNodeRuntimeState) ReleaseName() (*automa.EffectiveValue[string], error) {
	return br.releaseName.Effective()
}

func (br *BlockNodeRuntimeState) ChartName() (*automa.EffectiveValue[string], error) {
	return br.chartName.Effective()
}

func (br *BlockNodeRuntimeState) ChartRepo() (*automa.EffectiveValue[string], error) {
	return br.chartRepo.Effective()
}

func (br *BlockNodeRuntimeState) ChartVersion() (*automa.EffectiveValue[string], error) {
	return br.chartVersion.Effective()
}

// SetUserInputs sets the user input for the block-node runtime values.
func (br *BlockNodeRuntimeState) SetUserInputs(inputs models.BlocknodeInputs) error {
	if br.namespace == nil {
		return errorx.IllegalArgument.New("namespace runtime is not initialized") // should not happen
	}

	if err := br.setReleaseNameInput(inputs.Release); err != nil {
		return err
	}
	if err := br.setVersionInput(inputs.Version); err != nil {
		return err
	}
	if err := br.setNamespaceInput(inputs.Namespace); err != nil {
		return err
	}
	if err := br.setStorageInput(inputs.Storage); err != nil {
		return err
	}
	if err := br.setChartNameInput(inputs.ChartName); err != nil {
		return err
	}
	if err := br.setChartRefInput(inputs.Chart); err != nil {
		return err
	}
	if err := br.setChartVersionInput(inputs.ChartVersion); err != nil {
		return err
	}

	return nil
}

func (br *BlockNodeRuntimeState) setReleaseNameInput(r string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.releaseName == nil {
		return errorx.IllegalArgument.New("releaseName runtime is not initialized")
	}

	val, err := automa.NewValue(r)
	if err != nil {
		return err
	}

	br.releaseName.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) setNamespaceInput(ns string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.namespace == nil {
		return errorx.IllegalArgument.New("namespace runtime is not initialized") // should not happen
	}

	val, err := automa.NewValue(ns)
	if err != nil {
		return err
	}

	br.namespace.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) setVersionInput(v string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.version == nil {
		return errorx.IllegalArgument.New("version runtime is not initialized")
	}

	val, err := automa.NewValue(v)
	if err != nil {
		return err
	}

	br.version.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) setChartNameInput(c string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.chartName == nil {
		return errorx.IllegalArgument.New("chartName runtime is not initialized")
	}

	val, err := automa.NewValue(c)
	if err != nil {
		return err
	}

	br.chartName.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) setChartRefInput(c string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.chartRepo == nil {
		return errorx.IllegalArgument.New("chartRepo runtime is not initialized")
	}

	val, err := automa.NewValue(c)
	if err != nil {
		return err
	}

	br.chartRepo.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) setChartVersionInput(cv string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.chartVersion == nil {
		return errorx.IllegalArgument.New("chartVersion runtime is not initialized")
	}

	val, err := automa.NewValue(cv)
	if err != nil {
		return err
	}

	br.chartVersion.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) setStorageInput(s models.BlockNodeStorage) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.storage == nil {
		return errorx.IllegalArgument.New("storage runtime is not initialized") // should not happen
	}

	val, err := automa.NewValue(s)
	if err != nil {
		return err
	}

	br.storage.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntimeState) initNamespaceRuntime() error {
	var err error

	br.namespace, err = automa.NewRuntime[string](
		br.cfg.BlockNode.Namespace,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[string],
				userInput automa.Value[string],
			) (*automa.EffectiveValue[string], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.Namespace, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntimeState) initStorageRuntime() error {
	var err error

	br.storage, err = automa.NewRuntime[models.BlockNodeStorage](
		br.cfg.BlockNode.Storage,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[models.BlockNodeStorage],
				userInput automa.Value[models.BlockNodeStorage],
			) (*automa.EffectiveValue[models.BlockNodeStorage], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()

				eff, err2 := resolveEffectiveWithFunc[models.BlockNodeStorage](
					defaultVal,
					userInput,
					current.Storage,
					func() bool {
						return current.ReleaseInfo.Status == release.StatusDeployed
					},
					nil,
					func(v models.BlockNodeStorage) bool {
						return v.IsEmpty()
					},
				)

				if err2 != nil {
					return nil, false, err2
				}

				return eff, true, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntimeState) initVersionRuntime() error {
	var err error

	br.version, err = automa.NewRuntime[string](
		br.cfg.BlockNode.Version,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[string],
				userInput automa.Value[string],
			) (*automa.EffectiveValue[string], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.Version, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntimeState) initReleaseNameRuntime() error {
	var err error

	br.releaseName, err = automa.NewRuntime[string](
		br.cfg.BlockNode.Release,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[string],
				userInput automa.Value[string],
			) (*automa.EffectiveValue[string], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.Name, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntimeState) initChartNameRuntime() error {
	var err error

	br.chartName, err = automa.NewRuntime[string](
		br.cfg.BlockNode.ChartName,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[string],
				userInput automa.Value[string],
			) (*automa.EffectiveValue[string], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.ChartName, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntimeState) initChartRepoRuntime() error {
	var err error

	br.chartRepo, err = automa.NewRuntime[string](
		br.cfg.BlockNode.Chart,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[string],
				userInput automa.Value[string],
			) (*automa.EffectiveValue[string], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.ChartRef, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntimeState) initChartVersionRuntime() error {
	var err error

	br.chartVersion, err = automa.NewRuntime[string](
		br.cfg.BlockNode.ChartVersion,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[string],
				userInput automa.Value[string],
			) (*automa.EffectiveValue[string], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.ChartVersion, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

// InitBlockNodeRuntime initializes the block-node runtime.
// Caller needs to pass the current state, default config and reality checker.
// Caller also needs to set user inputs.
// The effective values of the block-node are determined based on the current state, default config and user inputs.
func InitBlockNodeRuntime(cfg models.Config, blockNodeState state.BlockNodeState, realityChecker reality.Checker, refreshInterval time.Duration) error {
	if realityChecker == nil {
		return errorx.IllegalArgument.New("reality checker cannot be nil")
	}

	rb := NewRuntimeBase[state.BlockNodeState](
		cfg,
		blockNodeState,
		refreshInterval,
		// fetch function
		realityChecker.BlockNodeState,
		// lastSync extractor
		func(s state.BlockNodeState) htime.Time { return s.LastSync },
		// clone helper
		func(s state.BlockNodeState) state.BlockNodeState { return s.Clone() },
		func() state.BlockNodeState { return state.BlockNodeState{} }, // default state
		"cluster reality checker",
	)

	br := &BlockNodeRuntimeState{
		Base:    rb,
		reality: realityChecker,
	}

	if err := br.initChartNameRuntime(); err != nil {
		return err
	}

	if err := br.initReleaseNameRuntime(); err != nil {
		return err
	}

	if err := br.initNamespaceRuntime(); err != nil {
		return err
	}

	if err := br.initVersionRuntime(); err != nil {
		return err
	}

	if err := br.initChartRepoRuntime(); err != nil {
		return err
	}

	if err := br.initChartVersionRuntime(); err != nil {
		return err
	}

	if err := br.initStorageRuntime(); err != nil {
		return err
	}

	blockNodeRuntimeSingleton = br

	return nil
}

func BlockNode() *BlockNodeRuntimeState {
	return blockNodeRuntimeSingleton
}
