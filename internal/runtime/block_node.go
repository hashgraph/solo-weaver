package runtime

import (
	"context"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

var blockNodeRuntimeSingleton *BlockNodeRuntime

type BlockNodeRuntime struct {
	*Base[core.BlockNodeState]
	reality      reality.Checker
	version      *automa.RuntimeValue[string]
	namespace    *automa.RuntimeValue[string]
	release      *automa.RuntimeValue[string]
	chartUrl     *automa.RuntimeValue[string]
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

func (br *BlockNodeRuntime) ChartUrl() (*automa.EffectiveValue[string], error) {
	return br.chartUrl.Effective()
}

func (br *BlockNodeRuntime) ChartVersion() (*automa.EffectiveValue[string], error) {
	return br.chartVersion.Effective()
}

// SetBlockNodeConfig sets the user input for the block-node runtime values.
func (br *BlockNodeRuntime) SetBlockNodeConfig(cfg config.Config) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	br.cfg = cfg

	if br.namespace == nil {
		return errorx.IllegalArgument.New("namespace runtime is not initialized") // should not happen
	}

	if err := br.SetNamespace(cfg.BlockNode.Namespace); err != nil {
		return err
	}
	if err := br.SetStorage(cfg.BlockNode.Storage); err != nil {
		return err
	}
	if err := br.SetVersion(cfg.BlockNode.Version); err != nil {
		return err
	}
	if err := br.SetRelease(cfg.BlockNode.Release); err != nil {
		return err
	}
	if err := br.SetChartUrl(cfg.BlockNode.ChartUrl); err != nil {
		return err
	}
	if err := br.SetChartVersion(cfg.BlockNode.ChartVersion); err != nil {
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

func (br *BlockNodeRuntime) SetChartUrl(c string) error {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.chartUrl == nil {
		return errorx.IllegalArgument.New("chartUrl runtime is not initialized")
	}

	val, err := automa.NewValue(c)
	if err != nil {
		return err
	}

	br.chartUrl.SetUserInput(val)
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

func (br *BlockNodeRuntime) initNamespaceRuntime() error {
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

func (br *BlockNodeRuntime) initStorageRuntime() error {
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
				return resolveEffective[config.BlockNodeStorage](defaultVal, userInput, current.Storage, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) initVersionRuntime() error {
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
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.Version, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) initReleaseRuntime() error {
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
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.Name, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) initChartUrlRuntime() error {
	var err error

	br.chartUrl, err = automa.NewRuntime[string](
		br.cfg.BlockNode.ChartUrl,
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
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.ChartUrl, current.ReleaseInfo.Status, true)
			},
		),
	)

	if err != nil {
		return err
	}
	return nil
}

func (br *BlockNodeRuntime) initChartVersionRuntime() error {
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
				return resolveEffective[string](defaultVal, userInput, current.ReleaseInfo.ChartVersion, current.ReleaseInfo.Status, true)
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

func InitBlockNodeRuntime(cfg config.Config, state core.BlockNodeState, realityChecker reality.Checker, refreshInterval time.Duration) error {
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

	br := &BlockNodeRuntime{
		Base:    rb,
		reality: realityChecker,
	}

	if err := br.initNamespaceRuntime(); err != nil {
		return err
	}
	if err := br.initStorageRuntime(); err != nil {
		return err
	}
	if err := br.initVersionRuntime(); err != nil {
		return err
	}
	if err := br.initReleaseRuntime(); err != nil {
		return err
	}
	if err := br.initChartUrlRuntime(); err != nil {
		return err
	}
	if err := br.initChartVersionRuntime(); err != nil {
		return err
	}

	blockNodeRuntimeSingleton = br

	return nil
}

func BlockNode() *BlockNodeRuntime {
	return blockNodeRuntimeSingleton
}
