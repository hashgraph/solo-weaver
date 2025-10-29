package helm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/joomcode/errorx"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"helm.sh/helm/v3/pkg/storage/driver"
)

type helmManager struct {
	log      zerolog.Logger
	settings *cli.EnvSettings
}

type Option func(*helmManager)

func WithLogger(log zerolog.Logger) Option {
	return func(h *helmManager) {
		h.log = log
	}
}

func WithSettings(settings *cli.EnvSettings) Option {
	return func(h *helmManager) {
		h.settings = settings
	}
}

// NewManager creates a new Helm manager
func NewManager(opts ...Option) (Manager, error) {
	m := &helmManager{
		log: zerolog.Nop(),
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.settings == nil {
		m.settings = cli.New()
	}

	return m, nil
}

func (h *helmManager) WithNamespace(namespace string) *cli.EnvSettings {
	settings := *h.settings
	settings.SetNamespace(namespace)
	return &settings
}

// AddRepo adds a Helm chart repository with the given options and updates it.
// It always forces updates the repo even if it already exists
// It is equivalent to: helm repo add <name> <url> && helm repo update <name>
func (h *helmManager) AddRepo(name, url string, o RepoAddOptions) (*repo.ChartRepository, error) {
	if o.RepoFile == "" {
		o.RepoFile = h.settings.RepositoryConfig
	}

	if o.RepoCache == "" {
		o.RepoCache = h.settings.RepositoryCache
	}

	h.log.Info().Str("name", name).Str("url", url).Msg("Adding Helm repository")

	// Ensure the file directory exists as it is required for file locking
	err := os.MkdirAll(filepath.Dir(o.RepoFile), os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return nil, err
	}

	// Acquire a file lock for process synchronization
	repoFileExt := filepath.Ext(o.RepoFile)
	var lockPath string
	if len(repoFileExt) > 0 && len(repoFileExt) < len(o.RepoFile) {
		lockPath = strings.TrimSuffix(o.RepoFile, repoFileExt) + ".lock"
	} else {
		lockPath = o.RepoFile + ".lock"
	}
	fileLock := flock.New(lockPath)
	lockCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer func() {
			e := fileLock.Unlock()
			if e != nil {
				h.log.Warn().Err(e).Str("lockPath", lockPath).Msg("failed to unlock repo file lock")
			}
		}()
	}

	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to acquire file lock for repo file %q", o.RepoFile)
	}

	b, err := os.ReadFile(o.RepoFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to read repo file %q", o.RepoFile)
	}

	var f repo.File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse repo file %q", o.RepoFile)
	}

	c := repo.Entry{
		Name: name,
		URL:  url,
	}

	// Check if the repo name is legal
	if strings.Contains(name, "/") {
		return nil, errorx.IllegalFormat.New("repository name (%s) contains '/', please specify a different name without '/'", name)
	}

	r, err := repo.NewChartRepository(&c, getter.All(h.settings))
	if err != nil {
		return nil, ErrRepoInvalid.Wrap(err, "failed to create chart repository for %q", url)
	}

	if o.RepoCache != "" {
		r.CachePath = o.RepoCache
	}

	if _, err := r.DownloadIndexFile(); err != nil {
		return r, errorx.InternalError.Wrap(err, "looks like %q is not a valid chart repository or cannot be reached", url)
	}

	f.Remove(name) // Remove if it already exists
	f.Update(&c)

	if err = f.WriteFile(o.RepoFile, 0600); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to write repo file %q", o.RepoFile)
	}

	h.log.Info().
		Str("name", name).
		Str("url", url).
		Str("repoFile", o.RepoFile).
		Str("RepoCache", o.RepoCache).
		Msg("Helm repository added successfully")

	return r, nil
}

// InstallChart installs a Helm chart with the given options
// It assumes that the release does not already exist
func (h *helmManager) InstallChart(ctx context.Context, releaseName, chartRef, chartVersion, namespace string, o InstallChartOptions) (*release.Release, error) {
	settings := h.WithNamespace(namespace)

	l := h.log.With().
		Str("releaseName", releaseName).
		Str("chartRef", chartRef).
		Str("namespace", settings.Namespace()).Logger()

	l.Info().Msg("Installing Helm chart")

	actionConfig, err := initActionConfig(settings, l.Printf)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}

	installClient := action.NewInstall(actionConfig)
	installClient.DryRunOption = "none"
	installClient.ReleaseName = releaseName
	installClient.Namespace = settings.Namespace()
	installClient.Version = chartVersion
	installClient.CreateNamespace = o.CreateNamespace
	installClient.Atomic = o.Atomic
	installClient.Wait = o.Wait
	installClient.WaitForJobs = true
	installClient.Timeout = o.Timeout
	if installClient.Timeout == 0 {
		installClient.Timeout = DefaultTimeout
	}

	registryClient, err := newRegistryClient(settings)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create registry client")
	}
	installClient.SetRegistryClient(registryClient)

	chartPath, err := installClient.ChartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, err
	}

	chartRequested, err := loader.Load(chartPath)
	if err != nil {
		return nil, ErrChartLoadFailed.Wrap(err, "failed to load chart")
	}

	providers := getter.All(settings)

	chartValues := map[string]interface{}{}
	if o.ValueOpts != nil {
		chartValues, err = o.ValueOpts.MergeValues(providers)
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "failed to merge chart values")
		}
	}

	// Check chart dependencies to make sure all are present in /charts
	if chartDependencies := chartRequested.Metadata.Dependencies; chartDependencies != nil {
		if err := action.CheckDependencies(chartRequested, chartDependencies); err != nil {
			err = fmt.Errorf("failed to check chart dependencies: %w", err)
			if !installClient.DependencyUpdate {
				return nil, err
			}

			manager := &downloader.Manager{
				Out:              l,
				ChartPath:        chartPath,
				Keyring:          installClient.ChartPathOptions.Keyring,
				SkipUpdate:       false,
				Getters:          providers,
				RepositoryConfig: settings.RepositoryConfig,
				RepositoryCache:  settings.RepositoryCache,
				Debug:            settings.Debug,
				RegistryClient:   installClient.GetRegistryClient(),
			}
			if err := manager.Update(); err != nil {
				return nil, err
			}
			// Reload the chart with the updated Chart.lock file.
			if chartRequested, err = loader.Load(chartPath); err != nil {
				return nil, fmt.Errorf("failed to reload chart after repo update: %w", err)
			}
		}
	}

	rel, err := installClient.RunWithContext(ctx, chartRequested, chartValues)
	if err != nil {
		return nil, ErrInstallFailed.Wrap(err, "failed to install chart")
	}

	l.Info().Msg("Helm chart installed successfully")

	return rel, nil
}

// UninstallChart uninstalls a Helm chart release by name in the specified namespace
// It returns ErrNotFound if the release does not exist
func (h *helmManager) UninstallChart(releaseName, namespace string) error {
	l := h.log.With().Str("releaseName", releaseName).Str("namespace", namespace).Logger()
	l.Info().Msg("Uninstalling Helm chart")

	settings := h.WithNamespace(namespace)

	actionConfig, err := initActionConfig(settings, l.Printf)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}

	uninstallClient := action.NewUninstall(actionConfig)
	uninstallClient.DeletionPropagation = "foreground" // "background" or "orphan"

	result, err := uninstallClient.Run(releaseName)
	if err != nil {
		return ErrUninstallFailed.Wrap(err, "failed to uninstall chart %q", releaseName)
	}

	if result == nil {
		return nil
	}

	l.Info().Any("info", result.Info).Msg("Uninstalled Helm release successfully")
	return nil
}

// UpgradeChart upgrades an existing Helm chart release
// It assumes that the release already exists
// If you need `helm upgrade --install`, use DeployChart instead
func (h *helmManager) UpgradeChart(ctx context.Context, releaseName, chartRef, chartVersion, namespace string, o UpgradeChartOptions) (*release.Release, error) {
	settings := h.WithNamespace(namespace)

	l := h.log.With().Str("releaseName", releaseName).
		Str("chartRef", chartRef).
		Str("namespace", settings.Namespace()).Logger()

	l.Info().Msg("Upgrading Helm chart")

	actionConfig, err := initActionConfig(settings, l.Printf)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}

	upgradeClient := action.NewUpgrade(actionConfig)

	upgradeClient.Namespace = settings.Namespace()
	upgradeClient.DryRunOption = "none"
	upgradeClient.WaitForJobs = true
	upgradeClient.Version = chartVersion
	upgradeClient.Atomic = o.Atomic
	upgradeClient.Wait = o.Wait
	upgradeClient.ReuseValues = o.ReuseValues
	upgradeClient.Timeout = o.Timeout

	// Set defaults if ValueOpts is not provided
	if o.ValueOpts == nil {
		upgradeClient.ReuseValues = true
	}

	if upgradeClient.Timeout == 0 {
		upgradeClient.Timeout = DefaultTimeout
	}

	registryClient, err := newRegistryClient(settings)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create registry client")
	}
	upgradeClient.SetRegistryClient(registryClient)

	chartPath, err := upgradeClient.ChartPathOptions.LocateChart(chartRef, settings)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to locate chart")
	}

	providers := getter.All(settings)

	// Check chart dependencies to make sure all are present in /charts
	chart, err := loader.Load(chartPath)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to load chart")
	}

	chartValues := map[string]interface{}{}
	if o.ValueOpts != nil {
		chartValues, err = o.ValueOpts.MergeValues(providers)
		if err != nil {
			return nil, err
		}
	}

	if req := chart.Metadata.Dependencies; req != nil {
		if err := action.CheckDependencies(chart, req); err != nil {
			err = fmt.Errorf("failed to check chart dependencies: %w", err)
			if !upgradeClient.DependencyUpdate {
				return nil, ErrChartDependencyMissing.Wrap(err, "chart dependency check failed")
			}

			man := &downloader.Manager{
				Out:              l,
				ChartPath:        chartPath,
				Keyring:          upgradeClient.ChartPathOptions.Keyring,
				SkipUpdate:       false,
				Getters:          providers,
				RepositoryConfig: settings.RepositoryConfig,
				RepositoryCache:  settings.RepositoryCache,
				Debug:            settings.Debug,
			}
			if err := man.Update(); err != nil {
				return nil, errorx.IllegalState.Wrap(err, "failed to update chart dependencies")
			}
			// Reload the chart with the updated Chart.lock file.
			if chart, err = loader.Load(chartPath); err != nil {
				return nil, errorx.IllegalState.Wrap(err, "failed to reload chart after repo update")
			}
		}
	}

	rel, err := upgradeClient.RunWithContext(ctx, releaseName, chart, chartValues)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, errorx.IllegalArgument.New("release %s not found — did you mean install?", releaseName)
		}
		return nil, ErrUpgradeFailed.Wrap(err, "failed to run upgrade action")
	}

	return rel, nil
}

// List lists Helm releases in the specified namespace
// If allNamespaces is true, it lists releases in all namespaces
// It only lists releases in deployed state
func (h *helmManager) List(namespace string, allNamespaces bool) ([]*release.Release, error) {
	h.log.Info().Str("namespace", namespace).Msg("Listing Helm releases")
	l := h.log.With().Str("namespace", namespace).Logger()

	settings := h.WithNamespace(namespace)
	actionConfig, err := initActionConfigList(settings, l.Printf, allNamespaces)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}

	listClient := action.NewList(actionConfig)

	// Only list deployed
	listClient.Deployed = true
	listClient.All = true
	listClient.SetStateMask()

	results, err := listClient.Run()
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to run list action")
	}

	return results, nil
}

// ListAll lists Helm releases in all namespaces
func (h *helmManager) ListAll() ([]*release.Release, error) {
	return h.List("", true)
}

// GetRelease retrieves a Helm release by name in the specified namespace
// It returns ErrNotFound if the release does not exist
func (h *helmManager) GetRelease(releaseName, namespace string) (*release.Release, error) {
	h.log.Info().
		Str("releaseName", releaseName).
		Str("namespace", namespace).
		Msg("Getting Helm release")

	settings := h.WithNamespace(namespace)
	actionConfig, err := initActionConfig(settings, h.log.Printf)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}

	statusClient := action.NewStatus(actionConfig)

	st, err := statusClient.Run(releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, ErrNotFound.Wrap(err, "release %q not found in namespace %q", releaseName, namespace)
		}
		return nil, errorx.InternalError.Wrap(err, "failed to get release status")
	}

	return st, nil
}

// IsInstalled checks if a Helm release is installed in the specified namespace
// It considers only releases in deployed state as "installed"
func (h *helmManager) IsInstalled(releaseName, namespace string) (bool, error) {
	h.log.Info().
		Str("releaseName", releaseName).
		Str("namespace", namespace).
		Msg("Checking if Helm release is installed")

	rel, err := h.GetRelease(releaseName, namespace)
	if err != nil {
		if errorx.IsOfType(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	// Consider only releases in deployed state as "installed"
	return rel.Info.Status == release.StatusDeployed, nil
}

// DeployChart installs or upgrades a Helm chart with the given options
// This is equivalent to "helm upgrade --install"
func (h *helmManager) DeployChart(ctx context.Context, releaseName, chartRef, chartVersion, namespace string, o DeployChartOptions) (*release.Release, error) {
	settings := h.WithNamespace(namespace)

	l := h.log.With().
		Str("releaseName", releaseName).
		Str("chartRef", chartRef).
		Str("namespace", settings.Namespace()).Logger()
	l.Info().Msg("Deploying Helm chart (install or upgrade)")

	actionConfig, err := initActionConfig(settings, l.Printf)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to init action config")
	}

	// Check if release exists
	statusClient := action.NewStatus(actionConfig)
	_, statusErr := statusClient.Run(releaseName)

	if statusErr != nil {
		// Release not found → Install
		if errors.Is(statusErr, driver.ErrReleaseNotFound) {
			l.Info().Msg("Release not found — installing chart")
			return h.InstallChart(ctx, releaseName, chartRef, chartVersion, namespace, InstallChartOptions{
				ValueOpts:       o.ValueOpts,
				CreateNamespace: o.CreateNamespace,
				Atomic:          o.Atomic,
				Wait:            o.Wait,
				Timeout:         o.Timeout,
			})
		}

		// Other failures when checking status
		return nil, errorx.IllegalState.Wrap(statusErr, "failed checking release status")
	}

	// Release exists → Upgrade
	l.Info().Msg("Release already exists — upgrading chart")
	return h.UpgradeChart(ctx, releaseName, chartRef, chartVersion, namespace, UpgradeChartOptions{
		ValueOpts:   o.ValueOpts,
		Atomic:      o.Atomic,
		Wait:        o.Wait,
		ReuseValues: o.ReuseValues,
		Timeout:     o.Timeout,
	})
}
