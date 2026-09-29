// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

// newConfigFakeClient builds a fake dynamic client that knows every ConsensusConfig
// GVR so Create/Get on the config kinds work.
func newConfigFakeClient(t *testing.T, objects ...runtime.Object) *fake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	listKinds := map[schema.GroupVersionResource]string{}
	register := func(e kindEntry) {
		gvk := schema.GroupVersionKind{Group: configCRGroup, Version: configCRVersion, Kind: e.kind}
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(e.kind+"List"), &unstructured.UnstructuredList{})
		listKinds[e.gvr] = e.kind + "List"
	}
	for _, e := range configFileKinds {
		register(e)
	}
	register(blockNodesEntry)
	return fake.NewSimpleDynamicClientWithCustomListKinds(scheme, listKinds, objects...)
}

// writePackage lays out an upgrade package under root: configFiles go in
// data/config/, rootFiles at the package root.
func writePackage(t *testing.T, root string, configFiles, rootFiles map[string]string) {
	t.Helper()
	cfgDir := filepath.Join(root, "data", "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	for name, content := range configFiles {
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, name), []byte(content), 0o644))
	}
	for name, content := range rootFiles {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(content), 0o644))
	}
}

// configCRWithValid builds a config CR carrying a Valid condition of the given status.
func configCRWithValid(name, kind, status string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": configCRGroup + "/" + configCRVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": name, "namespace": testNS},
		"status": map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{"type": "Valid", "status": status, "message": "operator says " + status},
			},
		},
	}}
}

func newTestDeployer(client *fake.FakeDynamicClient, upgradePath string) *configCRDeployer {
	return &configCRDeployer{
		client:       client,
		namespace:    testNS,
		upgradePath:  upgradePath,
		scope:        "node0",
		orbit:        testNS,
		nodeID:       "0",
		operationID:  testOperationID,
		pollInterval: time.Millisecond,
	}
}

func TestScanConfigFiles_RecognisedRootAndIgnored(t *testing.T) {
	root := t.TempDir()
	// genesis-network.json is ignored (skipped), throttles is recognised, log4j2.xml
	// is a recognised package-root file.
	writePackage(t, root,
		map[string]string{"throttles.json": "{}", "genesis-network.json": "{}"},
		map[string]string{"log4j2.xml": "<Configuration/>"})

	files, err := scanConfigFiles(root)
	require.NoError(t, err)

	kinds := map[string]bool{}
	for _, f := range files {
		kinds[f.entry.kind] = true
	}
	assert.True(t, kinds["ThrottlesConfig"], "throttles.json recognised")
	assert.True(t, kinds["Log4j2Config"], "log4j2.xml recognised at root")
	assert.Len(t, files, 2, "genesis-network.json is skipped, not counted")
}

func TestScanConfigFiles_UnrecognisedIsFatal(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, map[string]string{"mystery.conf": "x"}, nil)

	_, err := scanConfigFiles(root)
	require.Error(t, err)
	assert.False(t, errorx.IsTemporary(err), "an unrecognised file is fatal, not retried")
	r, ok := errx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, ReasonConfigFileUnrecognised, r)
}

func TestScanConfigFiles_SymlinkIsFatal(t *testing.T) {
	root := t.TempDir()
	cfgDir := filepath.Join(root, "data", "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))
	target := filepath.Join(root, "secret")
	require.NoError(t, os.WriteFile(target, []byte("s3cr3t"), 0o600))
	// Name the symlink after a recognised file so it is not rejected as unrecognised first.
	require.NoError(t, os.Symlink(target, filepath.Join(cfgDir, "throttles.json")))

	_, err := scanConfigFiles(root)
	require.Error(t, err)
	assert.False(t, errorx.IsTemporary(err))
	r, _ := errx.ReasonOf(err)
	assert.Equal(t, ReasonConfigFileSymlink, r)
}

func TestScanConfigFiles_AbsentDirIsNoError(t *testing.T) {
	files, err := scanConfigFiles(t.TempDir()) // no data/config/, no root files
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestConfigCRDeployer_CreateIdempotent(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root,
		map[string]string{"application.properties": "a=b", "throttles.json": "{}"},
		map[string]string{"log4j2.xml": "<Configuration/>"})
	client := newConfigFakeClient(t)
	d := newTestDeployer(client, root)
	ctx := context.Background()

	require.NoError(t, d.create(ctx))
	require.Len(t, d.refs, 3)

	// The application.properties CR carries the per-op name, content, scope, and label.
	name := strings.ToLower(testOperationID) + "-application-properties"
	obj, err := client.Resource(configGVR("applicationproperties")).Namespace(testNS).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err)
	content, _, _ := unstructured.NestedString(obj.Object, "spec", "content")
	scope, _, _ := unstructured.NestedString(obj.Object, "spec", "scope")
	orbit, _, _ := unstructured.NestedString(obj.Object, "spec", "orbit")
	assert.Equal(t, "a=b", content)
	assert.Equal(t, "node0", scope)
	assert.Equal(t, testNS, orbit)
	assert.Equal(t, testOperationID, obj.GetLabels()[labelOperationID])
	assert.Equal(t, createdByDaemon, obj.GetAnnotations()[annotationCreatedBy])

	// Re-run: AlreadyExists is success, no duplicate, no error.
	require.NoError(t, d.create(ctx), "create must be idempotent")
}

// writeBlockNodes writes this node's block-node config into the package.
func writeBlockNodes(t *testing.T, root, nodeID, content string) {
	t.Helper()
	dir := filepath.Join(root, "block-nodes", "config")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "block-nodes-"+nodeID+".json"), []byte(content), 0o644))
}

func TestConfigCRDeployer_CreateIncludesBlockNodes(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, map[string]string{"throttles.json": "{}"}, nil)
	writeBlockNodes(t, root, "0", `{"nodes":[]}`)
	client := newConfigFakeClient(t)
	d := newTestDeployer(client, root)
	ctx := context.Background()

	require.NoError(t, d.create(ctx))

	// The block-node CR uses the same per-operation naming as every other kind.
	name := strings.ToLower(testOperationID) + "-block-nodes"
	obj, err := client.Resource(configGVR("blocknodesconfigs")).Namespace(testNS).Get(ctx, name, metav1.GetOptions{})
	require.NoError(t, err, "per-operation BlockNodesConfig CR should exist")
	content, _, _ := unstructured.NestedString(obj.Object, "spec", "content")
	scope, _, _ := unstructured.NestedString(obj.Object, "spec", "scope")
	assert.Equal(t, `{"nodes":[]}`, content)
	assert.Equal(t, "node0", scope)
	assert.Equal(t, testOperationID, obj.GetLabels()[labelOperationID])

	names := make([]string, len(d.refs))
	for i, r := range d.refs {
		names[i] = r.name
	}
	assert.Contains(t, names, name, "block-node CR must be waited on alongside the others")
}

func TestConfigCRDeployer_CreateSkipsAbsentBlockNodes(t *testing.T) {
	root := t.TempDir()
	writePackage(t, root, map[string]string{"throttles.json": "{}"}, nil) // no block-nodes file
	client := newConfigFakeClient(t)
	d := newTestDeployer(client, root)

	require.NoError(t, d.create(context.Background()))
	assert.Len(t, d.refs, 1, "only throttles — absent block-node config is skipped, not an error")
}

func TestConfigCRDeployer_WaitValid_Success(t *testing.T) {
	name := strings.ToLower(testOperationID) + "-throttles"
	cr := configCRWithValid(name, "ThrottlesConfig", "True")
	client := newConfigFakeClient(t, cr)
	d := newTestDeployer(client, t.TempDir())
	d.refs = []configCRRef{{name: name, gvr: configGVR("throttlesconfigs"), kind: "ThrottlesConfig"}}

	require.NoError(t, d.waitValid(context.Background()))
}

func TestConfigCRDeployer_WaitValid_InvalidIsFatal(t *testing.T) {
	name := strings.ToLower(testOperationID) + "-throttles"
	cr := configCRWithValid(name, "ThrottlesConfig", "False")
	client := newConfigFakeClient(t, cr)
	d := newTestDeployer(client, t.TempDir())
	d.refs = []configCRRef{{name: name, gvr: configGVR("throttlesconfigs"), kind: "ThrottlesConfig"}}

	err := d.waitValid(context.Background())
	require.Error(t, err)
	assert.False(t, errorx.IsTemporary(err), "Valid=False is a terminal failure, not retried")
	r, ok := errx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, ReasonConfigCRInvalid, r, "reason must be the wire value ConfigCRInvalid")
}

func TestConfigCRDeployer_WaitValid_TimeoutIsTransient(t *testing.T) {
	name := strings.ToLower(testOperationID) + "-throttles"
	cr := configCRWithValid(name, "ThrottlesConfig", "Unknown") // never becomes True
	client := newConfigFakeClient(t, cr)
	d := newTestDeployer(client, t.TempDir())
	d.refs = []configCRRef{{name: name, gvr: configGVR("throttlesconfigs"), kind: "ThrottlesConfig"}}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := d.waitValid(ctx)
	require.Error(t, err)
	assert.True(t, errorx.IsTemporary(err), "a stalled reconcile is transient — retried until the handoff deadline")
}
