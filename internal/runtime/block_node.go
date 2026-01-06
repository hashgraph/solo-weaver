package runtime

import (
	"context"
	"reflect"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

var blockNodeRuntimeSingleton *BlockNodeRuntime

type BlockNodeRuntime struct {
	*Base[core.BlockNodeState]
	reality      reality.Checker
	version      *automa.RuntimeValue[string]
	namespace    *automa.RuntimeValue[string]
	release      *automa.RuntimeValue[string]
	chart        *automa.RuntimeValue[string]
	chartVersion *automa.RuntimeValue[string]
	storage      *automa.RuntimeValue[config.BlockNodeStorage]
}

func (br *BlockNodeRuntime) Namespace() (*automa.EffectiveValue[string], error) {
	return br.namespace.Effective()
}

func (br *BlockNodeRuntime) Storage() (*automa.EffectiveValue[config.BlockNodeStorage], error) {
	return br.storage.Effective()
}

func (br *BlockNodeRuntime) Version() (*automa.EffectiveValue[string], error) {
	return br.version.Effective()
}

func (br *BlockNodeRuntime) Release() (*automa.EffectiveValue[string], error) {
	return br.release.Effective()
}

func (br *BlockNodeRuntime) Chart() (*automa.EffectiveValue[string], error) {
	return br.chart.Effective()
}

func (br *BlockNodeRuntime) ChartVersion() (*automa.EffectiveValue[string], error) {
	return br.chartVersion.Effective()
}

// SetBlockNodeConfig sets the user input for the block-node runtime values.
func (br *BlockNodeRuntime) SetBlockNodeConfig(user config.BlockNodeConfig) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.namespace == nil {
		return errorx.IllegalArgument.New("namespace runtime is not initialized") // should not happen
	}

	if err := br.SetNamespace(user.Namespace); err != nil {
		return err
	}
	if err := br.SetStorage(user.Storage); err != nil {
		return err
	}
	if err := br.SetVersion(user.Version); err != nil {
		return err
	}
	if err := br.SetRelease(user.Release); err != nil {
		return err
	}
	if err := br.SetChart(user.ChartUrl); err != nil {
		return err
	}
	if err := br.SetChartVersion(user.ChartVersion); err != nil {
		return err
	}

	return nil
}

func (br *BlockNodeRuntime) SetNamespace(ns string) error {
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

func (br *BlockNodeRuntime) SetStorage(s config.BlockNodeStorage) error {
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

func (br *BlockNodeRuntime) SetVersion(v string) error {
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

func (br *BlockNodeRuntime) SetRelease(r string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.release == nil {
		return errorx.IllegalArgument.New("release runtime is not initialized")
	}

	val, err := automa.NewValue(r)
	if err != nil {
		return err
	}

	br.release.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntime) SetChart(c string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.chart == nil {
		return errorx.IllegalArgument.New("chart runtime is not initialized")
	}

	val, err := automa.NewValue(c)
	if err != nil {
		return err
	}

	br.chart.SetUserInput(val)
	return nil
}

func (br *BlockNodeRuntime) SetChartVersion(cv string) error {
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

func (br *BlockNodeRuntime) setNamespaceRuntime() error {
	var err error

	br.namespace, err = automa.NewRuntime[string](
		br.current.ReleaseInfo.Namespace,
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

				if userInput != nil && userInput.Val() != "" {
					val := userInput.Val()
					strategy := automa.StrategyUserInput

					// if block-node is already installed, use the current namespace
					if current.ReleaseInfo.Status == release.StatusDeployed && current.ReleaseInfo.Namespace != userInput.Val() {
						val = current.ReleaseInfo.Namespace
						strategy = automa.StrategyCurrent
					}

					ev, err := automa.NewEffective(val, strategy)
					if err != nil {
						return nil, false, err
					}
					return ev, true, nil
				}

				// default value
				ev, err := automa.NewEffectiveValue(defaultVal, automa.StrategyDefault)
				if err != nil {
					return nil, false, err
				}

				return ev, false, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) setStorageRuntime() error {
	var err error

	br.storage, err = automa.NewRuntime[config.BlockNodeStorage](
		br.current.Storage,
		automa.WithEffectiveFunc(
			func(
				ctx context.Context,
				defaultVal automa.Value[config.BlockNodeStorage],
				userInput automa.Value[config.BlockNodeStorage],
			) (*automa.EffectiveValue[config.BlockNodeStorage], bool, error) {
				// snapshot current under lock to avoid data races
				br.mu.Lock()
				current := br.current
				br.mu.Unlock()

				if userInput != nil {
					val := userInput.Val()
					strategy := automa.StrategyUserInput

					// if block-node is already created, do not allow changing storage:
					// return current storage with StrategyCustom instead of error
					if current.ReleaseInfo.Status == release.StatusDeployed && !reflect.DeepEqual(current.Storage, userInput.Val()) {
						val = current.Storage
						strategy = automa.StrategyCurrent
					}

					ev, err := automa.NewEffective(val, strategy)
					if err != nil {
						return nil, false, err
					}
					return ev, true, nil
				}

				// default value
				ev, err := automa.NewEffectiveValue(defaultVal, automa.StrategyDefault)
				if err != nil {
					return nil, false, err
				}

				return ev, false, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) setVersionRuntime() error {
	var err error

	br.version, err = automa.NewRuntime[string](
		br.current.ReleaseInfo.Version,
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

				if userInput != nil && userInput.Val() != "" {
					val := userInput.Val()
					strategy := automa.StrategyUserInput

					// if block-node is already created, use the current version
					if current.ReleaseInfo.Status == release.StatusDeployed && current.ReleaseInfo.Version != userInput.Val() {
						val = current.ReleaseInfo.Version
						strategy = automa.StrategyCurrent
					}

					ev, err := automa.NewEffective(val, strategy)
					if err != nil {
						return nil, false, err
					}
					return ev, true, nil
				}

				ev, err := automa.NewEffectiveValue(defaultVal, automa.StrategyDefault)
				if err != nil {
					return nil, false, err
				}
				return ev, false, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) setReleaseRuntime() error {
	var err error

	br.release, err = automa.NewRuntime[string](
		br.current.ReleaseInfo.Name,
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

				if userInput != nil && userInput.Val() != "" {
					val := userInput.Val()
					strategy := automa.StrategyUserInput

					// if block-node is already created, use the current release
					if current.ReleaseInfo.Status == release.StatusDeployed && current.ReleaseInfo.Name != userInput.Val() {
						val = current.ReleaseInfo.Name
						strategy = automa.StrategyCurrent
					}

					ev, err := automa.NewEffective(val, strategy)
					if err != nil {
						return nil, false, err
					}
					return ev, true, nil
				}

				ev, err := automa.NewEffectiveValue(defaultVal, automa.StrategyDefault)
				if err != nil {
					return nil, false, err
				}
				return ev, false, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) setChartRuntime() error {
	var err error

	br.chart, err = automa.NewRuntime[string](
		br.current.ReleaseInfo.ChartName,
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

				if userInput != nil && userInput.Val() != "" {
					val := userInput.Val()
					strategy := automa.StrategyUserInput

					// if block-node is already created, use the current chart
					if current.ReleaseInfo.Status == release.StatusDeployed && current.ReleaseInfo.ChartName != userInput.Val() {
						val = current.ReleaseInfo.ChartName
						strategy = automa.StrategyCurrent
					}

					ev, err := automa.NewEffective(val, strategy)
					if err != nil {
						return nil, false, err
					}
					return ev, true, nil
				}

				ev, err := automa.NewEffectiveValue(defaultVal, automa.StrategyDefault)
				if err != nil {
					return nil, false, err
				}
				return ev, false, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) setChartVersionRuntime() error {
	var err error

	br.chartVersion, err = automa.NewRuntime[string](
		br.current.ReleaseInfo.ChartVersion,
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

				if userInput != nil && userInput.Val() != "" {
					val := userInput.Val()
					strategy := automa.StrategyUserInput

					// if block-node is already created, use the current chart version
					if current.ReleaseInfo.Status == release.StatusDeployed && current.ReleaseInfo.ChartVersion != userInput.Val() {
						val = current.ReleaseInfo.ChartVersion
						strategy = automa.StrategyCurrent
					}

					ev, err := automa.NewEffective(val, strategy)
					if err != nil {
						return nil, false, err
					}
					return ev, true, nil
				}

				ev, err := automa.NewEffectiveValue(defaultVal, automa.StrategyDefault)
				if err != nil {
					return nil, false, err
				}
				return ev, false, nil
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) RefreshInterval() time.Duration {
	return br.refreshInterval
}

func (br *BlockNodeRuntime) SetRefreshInterval(d time.Duration) {
	br.refreshInterval = d
}

func InitBlockNodeRuntime(state core.BlockNodeState, realityChecker reality.Checker, refreshInterval time.Duration) error {
	if realityChecker == nil {
		return errorx.IllegalArgument.New("reality checker cannot be nil")
	}

	rb := NewRuntimeBase[core.BlockNodeState](
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

	br := &BlockNodeRuntime{
		Base:    rb,
		reality: realityChecker,
	}

	if err := br.setNamespaceRuntime(); err != nil {
		return err
	}
	if err := br.setStorageRuntime(); err != nil {
		return err
	}
	if err := br.setVersionRuntime(); err != nil {
		return err
	}
	if err := br.setReleaseRuntime(); err != nil {
		return err
	}
	if err := br.setChartRuntime(); err != nil {
		return err
	}
	if err := br.setChartVersionRuntime(); err != nil {
		return err
	}

	blockNodeRuntimeSingleton = br

	return nil
}

func BlockNode() *BlockNodeRuntime {
	return blockNodeRuntimeSingleton
}
