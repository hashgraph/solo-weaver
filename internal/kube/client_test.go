// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/testutil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseManifests(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantCount   int
		validateFns []func(*testing.T, *unstructured.Unstructured)
	}{
		{
			name:      "single document",
			content:   "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: single-ns\n",
			wantCount: 1,
			validateFns: []func(*testing.T, *unstructured.Unstructured){
				func(t *testing.T, u *unstructured.Unstructured) {
					if u.GetKind() != "Namespace" {
						t.Fatalf("expected kind Namespace, got %s", u.GetKind())
					}
					if u.GetName() != "single-ns" {
						t.Fatalf("expected name single-ns, got %s", u.GetName())
					}
				},
			},
		},
		{
			name: "multi document",
			content: `apiVersion: v1
kind: Namespace
metadata:
  name: ns1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
data:
  key: value
`,
			wantCount: 2,
			validateFns: []func(*testing.T, *unstructured.Unstructured){
				func(t *testing.T, u *unstructured.Unstructured) {
					if u.GetKind() != "Namespace" || u.GetName() != "ns1" {
						t.Fatalf("unexpected first object: kind=%s name=%s", u.GetKind(), u.GetName())
					}
				},
				func(t *testing.T, u *unstructured.Unstructured) {
					if u.GetKind() != "ConfigMap" || u.GetName() != "cm1" {
						t.Fatalf("unexpected second object: kind=%s name=%s", u.GetKind(), u.GetName())
					}
					data, found, _ := unstructured.NestedStringMap(u.Object, "data")
					if !found || data["key"] != "value" {
						t.Fatalf("configmap data mismatch: %#v", data)
					}
				},
			},
		},
		{
			name: "empty docs filtered",
			content: `
---
# empty doc above, real doc below

apiVersion: v1
kind: Namespace
metadata:
  name: with-empty
`,
			wantCount: 1,
			validateFns: []func(*testing.T, *unstructured.Unstructured){
				func(t *testing.T, u *unstructured.Unstructured) {
					if u.GetKind() != "Namespace" || u.GetName() != "with-empty" {
						t.Fatalf("unexpected object from empty-doc test: kind=%s name=%s", u.GetKind(), u.GetName())
					}
				},
			},
		},
		{
			name:      "json document",
			content:   `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"json-ns"}}`,
			wantCount: 1,
			validateFns: []func(*testing.T, *unstructured.Unstructured){
				func(t *testing.T, u *unstructured.Unstructured) {
					if u.GetKind() != "Namespace" || u.GetName() != "json-ns" {
						t.Fatalf("unexpected json object: kind=%s name=%s", u.GetKind(), u.GetName())
					}
				},
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path, cleanup := testutil.WriteTempManifest(t, tc.content)
			defer cleanup()

			objs, err := parseManifests(path)
			if err != nil {
				t.Fatalf("parseManifests returned error: %v", err)
			}
			if len(objs) != tc.wantCount {
				t.Fatalf("expected %d objects, got %d", tc.wantCount, len(objs))
			}
			// run validators
			if len(tc.validateFns) != len(objs) {
				// defensive: allow fewer validators
				if len(tc.validateFns) > len(objs) {
					t.Fatalf("not enough parsed objects for validators: have %d objects, %d validators", len(objs), len(tc.validateFns))
				}
			}
			for i, fn := range tc.validateFns {
				fn(t, objs[i])
			}

			// Additional sanity: ensure returned objects are distinct maps (no shared underlying map)
			if len(objs) > 1 {
				if reflect.ValueOf(objs[0].Object).Pointer() == reflect.ValueOf(objs[1].Object).Pointer() {
					t.Fatalf("parsed objects share underlying map pointer")
				}
			}
		})
	}
}

// TestSleepWithContext_Succeeds ensures sleepWithContext returns nil and sleeps approximately the requested duration.
func TestSleepWithContext_Succeeds(t *testing.T) {
	t.Parallel()
	d := 40 * time.Millisecond
	start := time.Now()
	if err := sleepWithContext(context.Background(), d); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	elapsed := time.Since(start)
	// allow some scheduling leeway; ensure we slept at least half of the requested duration
	if elapsed < d/2 {
		t.Fatalf("slept less than expected: elapsed=%v want>=%v", elapsed, d/2)
	}
}

// TestSleepWithContext_Cancelled ensures sleepWithContext returns early with context.Canceled when the context is cancelled.
func TestSleepWithContext_Cancelled(t *testing.T) {
	t.Parallel()
	d := 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// cancel shortly after starting sleepWithContext
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := sleepWithContext(ctx, d)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected error due to cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	// ensure it returned early (well before the full duration)
	if elapsed >= d {
		t.Fatalf("did not return early on cancel: elapsed=%v timeout=%v", elapsed, d)
	}
}

func TestToResourceKind(t *testing.T) {
	tests := []struct {
		name string
		gvr  schema.GroupVersionResource
		want ResourceKind
	}{
		{"namespaces", schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, KindNamespace},
		{"configmaps", schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, KindConfigMap},
		{"pods", schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, KindPod},
		{"deployments", schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, KindDeployment},
		{"jobs", schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, KindJob},
		{"pvcs", schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}, KindPVC},
		{"unknown", schema.GroupVersionResource{Group: "", Version: "v1", Resource: "foos"}, ResourceKind("foos")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ToResourceKind(tc.gvr)
			if got != tc.want {
				t.Fatalf("ToResourceKind(%v) = %q, want %q", tc.gvr, got, tc.want)
			}
			// round-trip: string representation stays consistent
			if got.String() != string(got) {
				t.Fatalf("ResourceKind.String mismatch: got=%q", got.String())
			}
		})
	}
}

func TestToGroupVersionResource(t *testing.T) {
	tests := []struct {
		name    string
		kind    ResourceKind
		wantGVR schema.GroupVersionResource
		wantErr bool
	}{
		{"Namespace", KindNamespace, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, false},
		{"ConfigMap", KindConfigMap, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, false},
		{"Pod", KindPod, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, false},
		{"Deployment", KindDeployment, schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, false},
		{"Job", KindJob, schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, false},
		{"PVC", KindPVC, schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}, false},
		{"UnknownKind", ResourceKind("DoesNotExist"), schema.GroupVersionResource{}, true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gvr, err := ToGroupVersionResource(tc.kind)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for kind %q, got nil", tc.kind)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(gvr, tc.wantGVR) {
				t.Fatalf("ToGroupVersionResource(%q) = %v, want %v", tc.kind, gvr, tc.wantGVR)
			}
		})
	}
}

func TestToPhase(t *testing.T) {
	tests := []struct {
		in   string
		want Phase
	}{
		{"Pending", PhasePending},
		{"Running", PhaseRunning},
		{"Succeeded", PhaseSucceeded},
		{"Failed", PhaseFailed},
		{"Unknown", PhaseUnknown},
		{"CustomPhase", Phase("CustomPhase")},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			got := ToPhase(tc.in)
			if got != tc.want {
				t.Fatalf("ToPhase(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// ensure String returns the original textual form
			if got.String() != string(got) {
				t.Fatalf("Phase.String mismatch: got=%q", got.String())
			}
		})
	}
}

// TestWaitOptions_AsListOptions verifies that WaitOptions correctly converts to ListOptions
func TestWaitOptions_AsListOptions(t *testing.T) {
	tests := []struct {
		name     string
		opts     WaitOptions
		wantList string
		wantFld  string
	}{
		{
			name:     "empty options",
			opts:     WaitOptions{},
			wantList: "",
			wantFld:  "",
		},
		{
			name:     "label selector only",
			opts:     WaitOptions{LabelSelector: "app=test"},
			wantList: "app=test",
			wantFld:  "",
		},
		{
			name:     "field selector only",
			opts:     WaitOptions{FieldSelector: "metadata.name=foo"},
			wantList: "",
			wantFld:  "metadata.name=foo",
		},
		{
			name:     "both selectors",
			opts:     WaitOptions{LabelSelector: "app=test", FieldSelector: "metadata.name=foo"},
			wantList: "app=test",
			wantFld:  "metadata.name=foo",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			lo := tc.opts.AsListOptions()
			if lo.LabelSelector != tc.wantList {
				t.Errorf("LabelSelector = %q, want %q", lo.LabelSelector, tc.wantList)
			}
			if lo.FieldSelector != tc.wantFld {
				t.Errorf("FieldSelector = %q, want %q", lo.FieldSelector, tc.wantFld)
			}
		})
	}
}

// TestWaitOptions_String verifies the string representation of WaitOptions
func TestWaitOptions_String(t *testing.T) {
	tests := []struct {
		name string
		opts WaitOptions
		want string
	}{
		{
			name: "empty",
			opts: WaitOptions{},
			want: "[]",
		},
		{
			name: "name prefix only",
			opts: WaitOptions{NamePrefix: "test-"},
			want: "[namePrefix=test-]",
		},
		{
			name: "label selector only",
			opts: WaitOptions{LabelSelector: "app=test"},
			want: "[labelSelector=app=test]",
		},
		{
			name: "all options",
			opts: WaitOptions{
				NamePrefix:    "test-",
				LabelSelector: "app=test",
				FieldSelector: "metadata.name=foo",
			},
			want: "[namePrefix=test-, labelSelector=app=test, fieldSelector=metadata.name=foo]",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := tc.opts.String()
			if got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetResourceNestedString(t *testing.T) {
	tests := []struct {
		name      string
		obj       map[string]interface{}
		fields    []string
		wantValue string
		wantFound bool
		wantErr   bool
	}{
		{
			name: "simple nested string",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"provider": map[string]interface{}{
						"vault": map[string]interface{}{
							"server": "https://vault.example.com:8200",
						},
					},
				},
			},
			fields:    []string{"spec", "provider", "vault", "server"},
			wantValue: "https://vault.example.com:8200",
			wantFound: true,
		},
		{
			name: "top level field",
			obj: map[string]interface{}{
				"name": "test-resource",
			},
			fields:    []string{"name"},
			wantValue: "test-resource",
			wantFound: true,
		},
		{
			name: "missing field returns empty",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"provider": map[string]interface{}{},
				},
			},
			fields:    []string{"spec", "provider", "vault", "server"},
			wantValue: "",
			wantFound: false,
		},
		{
			name: "missing intermediate path returns empty",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{},
			},
			fields:    []string{"spec", "provider", "vault", "server"},
			wantValue: "",
			wantFound: false,
		},
		{
			name:      "empty object returns empty",
			obj:       map[string]interface{}{},
			fields:    []string{"spec", "provider"},
			wantValue: "",
			wantFound: false,
		},
		{
			name: "non-string value returns error",
			obj: map[string]interface{}{
				"spec": map[string]interface{}{
					"count": 42,
				},
			},
			fields:  []string{"spec", "count"},
			wantErr: true,
		},
		{
			name: "deeply nested value",
			obj: map[string]interface{}{
				"a": map[string]interface{}{
					"b": map[string]interface{}{
						"c": map[string]interface{}{
							"d": map[string]interface{}{
								"e": "deep-value",
							},
						},
					},
				},
			},
			fields:    []string{"a", "b", "c", "d", "e"},
			wantValue: "deep-value",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, found, err := unstructured.NestedString(tt.obj, tt.fields...)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
			if value != tt.wantValue {
				t.Errorf("value = %q, want %q", value, tt.wantValue)
			}
		})
	}
}

// ── resolveKubeconfigPath ─────────────────────────────────────────────────────

func TestResolveKubeconfigPath_InCluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "/tmp/no-such-home")

	got := resolveKubeconfigPath()
	if got != "in-cluster" {
		t.Errorf("in-cluster: got %q, want %q", got, "in-cluster")
	}
}

func TestResolveKubeconfigPath_KubeconfigEnv(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", "/custom/path/kubeconfig")

	got := resolveKubeconfigPath()
	if got != "/custom/path/kubeconfig" {
		t.Errorf("KUBECONFIG env: got %q, want %q", got, "/custom/path/kubeconfig")
	}
}

func TestResolveKubeconfigPath_Default(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "/home/testuser")

	got := resolveKubeconfigPath()
	want := "/home/testuser/.kube/config"
	if got != want {
		t.Errorf("default: got %q, want %q", got, want)
	}
}

func TestResolveKubeconfigPath_NoHome(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", "")

	got := resolveKubeconfigPath()
	if got != "" {
		t.Errorf("no HOME: got %q, want empty string", got)
	}
}

// ── ClusterExists fast-path ───────────────────────────────────────────────────

// TestClusterExists_ReturnsFalseImmediately_WhenNoKubeconfig verifies the
// Stage-1 fast path: if the kubeconfig file is absent ClusterExists returns
// (false, nil) without making any network call.
func TestClusterExists_ReturnsFalseImmediately_WhenNoKubeconfig(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", "")
	t.Setenv("HOME", t.TempDir()) // temp dir has no .kube/config

	exists, err := ClusterExists()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if exists {
		t.Fatal("expected false when kubeconfig is absent")
	}
}

// TestClusterExists_ReturnsFalseImmediately_WhenKubeconfigMissing verifies
// that pointing KUBECONFIG at a non-existent file returns (false, nil) without
// a network call.
func TestClusterExists_ReturnsFalseImmediately_WhenKubeconfigMissing(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "no-such-kubeconfig"))

	exists, err := ClusterExists()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if exists {
		t.Fatal("expected false when kubeconfig file does not exist")
	}
}

// TestClusterExists_ReturnsFalseImmediately_WhenKubeconfigEmpty verifies that
// an empty (unparseable) kubeconfig file does not block on the network.
func TestClusterExists_ReturnsFalseImmediately_WhenKubeconfigEmpty(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "config")
	if err := os.WriteFile(kubeconfig, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBECONFIG", kubeconfig)

	exists, err := ClusterExists()
	if err != nil {
		t.Fatalf("expected nil error for empty kubeconfig, got: %v", err)
	}
	if exists {
		t.Fatal("expected false for empty/unparseable kubeconfig")
	}
}
