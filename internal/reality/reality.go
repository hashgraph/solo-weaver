// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	helm2 "github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Checker is the composite abstraction for accessing the current state of the
type Checker[T any] interface {
	RefreshState(ctx context.Context) (T, error)
}

// Checkers is the aggregate of all reality checkers, providing a single entry point for callers to access the
// current state of the cluster, machines, and block node.
type Checkers struct {
	Cluster   Checker[state.ClusterState]
	Machine   Checker[state.MachineState]
	BlockNode Checker[state.BlockNodeState]
}

// ---------------------------------------------------------------------------
// Injectable dependency types
// ---------------------------------------------------------------------------

// HelmManager is the subset of helm2.Manager used by the BlockNode checker.
// Swap in a fake for unit tests.
type HelmManager interface {
	ListAll() ([]*release.Release, error)
}

// KubeClient is the subset of kube.Client used by the BlockNode checker.
type KubeClient interface {
	List(ctx context.Context, kind kube.ResourceKind, namespace string, opts kube.WaitOptions) (*unstructured.UnstructuredList, error)
}

// ClusterProbe abstracts the package-level kube.ClusterExists check.
// Exported so callers can provide fakes in tests.
type ClusterProbe func() (bool, error)

// ---------------------------------------------------------------------------
// CheckerOption — functional options applied to all sub-checkers
// ---------------------------------------------------------------------------

// checkerConfig holds all options before the sub-checkers are constructed.
type checkerConfig struct {
	sm            state.Manager
	sandboxBinDir string
	stateDir      string
	newHelm       func() (HelmManager, error)
	newKube       func() (KubeClient, error)
	clusterExists ClusterProbe
}

// CheckerOption configures the Checker.
type CheckerOption func(*checkerConfig)

func WithSandboxBinDir(dir string) CheckerOption {
	return func(c *checkerConfig) { c.sandboxBinDir = dir }
}

func WithStateDir(dir string) CheckerOption {
	return func(c *checkerConfig) { c.stateDir = dir }
}

func WithHelmFactory(fn func() (HelmManager, error)) CheckerOption {
	return func(c *checkerConfig) { c.newHelm = fn }
}

func WithKubeFactory(fn func() (KubeClient, error)) CheckerOption {
	return func(c *checkerConfig) { c.newKube = fn }
}

func WithClusterProbe(fn ClusterProbe) CheckerOption {
	return func(c *checkerConfig) { c.clusterExists = fn }
}

// ---------------------------------------------------------------------------
// NewCheckers — production factory
// ---------------------------------------------------------------------------

// NewCheckers constructs a Checker composed of three focused sub-checkers.
// Production defaults are applied; use CheckerOption to override for tests.
func NewCheckers(sm state.Manager, opts ...CheckerOption) (Checkers, error) {
	cc := &checkerConfig{
		sm:            sm,
		newHelm:       func() (HelmManager, error) { return helm2.NewManager() },
		newKube:       func() (KubeClient, error) { return kube.NewClient() },
		clusterExists: kube.ClusterExists,
	}
	for _, o := range opts {
		o(cc)
	}

	cluster, err := NewClusterChecker(cc.sm, cc.clusterExists)
	if err != nil {
		return Checkers{}, errorx.IllegalState.Wrap(err, "failed to create cluster checker")
	}

	machine, err := NewMachineChecker(cc.sm, cc.sandboxBinDir, cc.stateDir)
	if err != nil {
		return Checkers{}, errorx.IllegalState.Wrap(err, "failed to create machine checker")
	}

	blocknode, err := NewBlockNodeChecker(cc.sm, cc.newHelm, cc.newKube, cc.clusterExists)
	if err != nil {
		return Checkers{}, errorx.IllegalState.Wrap(err, "failed to create block node checker")
	}

	return Checkers{
		Cluster:   cluster,
		Machine:   machine,
		BlockNode: blocknode,
	}, nil
}
