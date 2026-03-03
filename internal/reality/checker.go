// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"io"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	helm2 "github.com/hashgraph/solo-weaver/pkg/helm"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// ---------------------------------------------------------------------------
// Public sub-checker interfaces
// ---------------------------------------------------------------------------

// ClusterChecker probes the Kubernetes cluster state.
type ClusterChecker interface {
	ClusterState(ctx context.Context) (state.ClusterState, error)
}

// MachineChecker probes the local machine state (software + hardware).
type MachineChecker interface {
	MachineState(ctx context.Context) (state.MachineState, error)
}

// BlockNodeChecker probes the BlockNode deployment state.
type BlockNodeChecker interface {
	BlockNodeState(ctx context.Context) (state.BlockNodeState, error)
}

// Checker is the composite abstraction for accessing the current state of the
// system including cluster, machines, and blocknodes.
//
// It is used by the BLL to make decisions based on the actual state of the system.
// It is separate from the StateManager and BLL which manages the current state and executes intents.
//
// This separation allows for a clearer distinction between current and actual states as well as intent execution.
// This is supposed to be resource-heavy and may involve network calls, so it should be used judiciously.
type Checker interface {
	ClusterChecker
	MachineChecker
	BlockNodeChecker
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
// Composite checker — delegates to three focused implementations
// ---------------------------------------------------------------------------

type compositeChecker struct {
	cluster   ClusterChecker
	machine   MachineChecker
	blocknode BlockNodeChecker
}

// Ensure compositeChecker satisfies the Checker interface at compile time.
var _ Checker = (*compositeChecker)(nil)

func (c *compositeChecker) ClusterState(ctx context.Context) (state.ClusterState, error) {
	return c.cluster.ClusterState(ctx)
}

func (c *compositeChecker) MachineState(ctx context.Context) (state.MachineState, error) {
	return c.machine.MachineState(ctx)
}

func (c *compositeChecker) BlockNodeState(ctx context.Context) (state.BlockNodeState, error) {
	return c.blocknode.BlockNodeState(ctx)
}

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
// NewChecker — production factory
// ---------------------------------------------------------------------------

// NewChecker constructs a Checker composed of three focused sub-checkers.
// Production defaults are applied; use CheckerOption to override for tests.
func NewChecker(sm state.Manager, opts ...CheckerOption) (Checker, error) {
	cfg := &checkerConfig{
		sm:            sm,
		newHelm:       func() (HelmManager, error) { return helm2.NewManager() },
		newKube:       func() (KubeClient, error) { return kube.NewClient() },
		clusterExists: kube.ClusterExists,
	}
	for _, o := range opts {
		o(cfg)
	}

	cluster := newClusterChecker(cfg.clusterExists)
	machine := newMachineChecker(cfg.sm, cfg.sandboxBinDir, cfg.stateDir)
	blocknode := newBlockNodeChecker(cfg.newHelm, cfg.newKube, cfg.clusterExists)

	return &compositeChecker{
		cluster:   cluster,
		machine:   machine,
		blocknode: blocknode,
	}, nil
}

// ---------------------------------------------------------------------------
// UnmarshalManifest — shared manifest helper used by blockNodeChecker
// ---------------------------------------------------------------------------

// UnmarshalManifest parses a Helm release manifest (possibly multi-doc YAML)
// and returns a slice of Unstructured objects (one per non-empty document).
func UnmarshalManifest(manifest string) ([]*unstructured.Unstructured, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured

	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		// skip empty documents (e.g. separators or whitespace-only)
		if len(doc) == 0 {
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: doc})
	}
	return out, nil
}
