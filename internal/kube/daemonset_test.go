// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"errors"
	"testing"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func dsStatusObj(generation, observed, desired, ready, updated int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "DaemonSet",
			"metadata": map[string]interface{}{
				"name":       "cilium",
				"namespace":  "kube-system",
				"generation": generation,
			},
			"status": map[string]interface{}{
				"observedGeneration":     observed,
				"desiredNumberScheduled": desired,
				"numberReady":            ready,
				"updatedNumberScheduled": updated,
			},
		},
	}
}

func TestIsDaemonSetRolledOut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		obj     *unstructured.Unstructured
		getErr  error
		want    bool
		wantErr bool
	}{
		{
			name: "fully rolled out",
			obj:  dsStatusObj(2, 2, 3, 3, 3),
			want: true,
		},
		{
			name: "observed generation behind",
			obj:  dsStatusObj(2, 1, 3, 3, 3),
			want: false,
		},
		{
			name: "ready replicas behind",
			obj:  dsStatusObj(2, 2, 3, 2, 3),
			want: false,
		},
		{
			name: "updated replicas behind (old pods still running)",
			obj:  dsStatusObj(2, 2, 3, 3, 2),
			want: false,
		},
		{
			name: "desiredNumberScheduled is zero (no selected nodes)",
			obj:  dsStatusObj(1, 1, 0, 0, 0),
			want: true,
		},
		{
			name:   "NotFound error → false, no error",
			obj:    nil,
			getErr: kerrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "daemonsets"}, "cilium"),
			want:   false,
		},
		{
			name:    "transient error propagates",
			obj:     nil,
			getErr:  errors.New("transient API failure"),
			want:    false,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := IsDaemonSetRolledOut(tc.obj, tc.getErr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("IsDaemonSetRolledOut = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKindDaemonSet_GVRMapping(t *testing.T) {
	t.Parallel()
	gvr, err := ToGroupVersionResource(KindDaemonSet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	if gvr != want {
		t.Fatalf("KindDaemonSet GVR = %v, want %v", gvr, want)
	}
	if back := ToResourceKind(gvr); back != KindDaemonSet {
		t.Fatalf("round-trip ToResourceKind = %v, want %v", back, KindDaemonSet)
	}
}
