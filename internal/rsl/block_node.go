package rsl

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

var blockNodeRuntimeSingleton *BlockNodeRuntimeState

type BlockNodeRuntimeState struct {
	*Base[core.BlockNodeState]
	releaseName  *automa.RuntimeValue[string]
	reality      reality.Checker
	version      *automa.RuntimeValue[string]
	namespace    *automa.RuntimeValue[string]
	chartName    *automa.RuntimeValue[string]
	chartRepo    *automa.RuntimeValue[string]
	chartVersion *automa.RuntimeValue[string]
	storage      *automa.RuntimeValue[core.BlockNodeStorage]
}

func (br *BlockNodeRuntimeState) Namespace() (*automa.EffectiveValue[string], error) {
	return br.namespace.Effective()
}

func (br *BlockNodeRuntimeState) Storage() (*automa.EffectiveValue[core.BlockNodeStorage], error) {
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

// SetBlockNodeConfig sets the user input for the block-node runtime values.
func (br *BlockNodeRuntimeState) SetBlockNodeConfig(cfg core.Config) error {
	if br.namespace == nil {
		return errorx.IllegalArgument.New("namespace runtime is not initialized") // should not happen
	}

	br.mu.Lock()
	br.cfg = cfg
	br.mu.Unlock()

	if err := br.SetReleaseName(cfg.BlockNode.Release); err != nil {
		return err
	}
	if err := br.SetVersion(cfg.BlockNode.Version); err != nil {
		return err
	}
	if err := br.SetNamespace(cfg.BlockNode.Namespace); err != nil {
		return err
	}
	if err := br.SetStorage(cfg.BlockNode.Storage); err != nil {
		return err
	}
	if err := br.SetChartName(cfg.BlockNode.ChartName); err != nil {
		return err
	}
	if err := br.SetChartRef(cfg.BlockNode.Chart); err != nil {
		return err
	}
	if err := br.SetChartVersion(cfg.BlockNode.ChartVersion); err != nil {
		return err
	}

	return nil
}

func (br *BlockNodeRuntimeState) SetReleaseName(r string) error {
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

func (br *BlockNodeRuntimeState) SetNamespace(ns string) error {
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

func (br *BlockNodeRuntimeState) SetVersion(v string) error {
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

func (br *BlockNodeRuntimeState) SetChartName(c string) error {
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

func (br *BlockNodeRuntimeState) SetChartRef(c string) error {
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

func (br *BlockNodeRuntimeState) SetChartVersion(cv string) error {
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

func (br *BlockNodeRuntimeState) SetStorage(s core.BlockNodeStorage) error {
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

	br.storage, err = automa.NewRuntime[core.BlockNodeStorage](
		br.cfg.BlockNode.Storage,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[core.BlockNodeStorage],
				userInput automa.Value[core.BlockNodeStorage],
			) (*automa.EffectiveValue[core.BlockNodeStorage], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()

				eff, err2 := resolveEffectiveWithFunc[core.BlockNodeStorage](
					defaultVal,
					userInput,
					current.Storage,
					func() bool {
						return current.ReleaseInfo.Status == release.StatusDeployed
					},
					nil,
					func(v core.BlockNodeStorage) bool {
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

func (br *BlockNodeRuntimeState) RefreshInterval() time.Duration {
	return br.refreshInterval
}

func (br *BlockNodeRuntimeState) SetRefreshInterval(d time.Duration) {
	br.refreshInterval = d
}

func InitBlockNodeRuntime(cfg core.Config, state core.BlockNodeState, realityChecker reality.Checker, refreshInterval time.Duration) error {
	if realityChecker == nil {
		return errorx.IllegalArgument.New("reality checker cannot be nil")
	}

	rb := NewRuntimeBase[core.BlockNodeState](
		cfg,
		state,
		refreshInterval,
		// fetch function
		realityChecker.BlockNodeState,
		// lastSync extractor
		func(s *core.BlockNodeState) htime.Time { return s.LastSync },
		// clone helper
		func(s *core.BlockNodeState) *core.BlockNodeState { return s.Clone() },
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
