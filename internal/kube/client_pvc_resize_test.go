// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func storageAPIResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{
		{
			GroupVersion: "storage.k8s.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "storageclasses", SingularName: "storageclass", Namespaced: false, Kind: "StorageClass"},
			},
		},
	}
}

func newStorageClass(name string, allowExpansion *bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]interface{}{}}
	u.SetAPIVersion("storage.k8s.io/v1")
	u.SetKind("StorageClass")
	u.SetName(name)
	if allowExpansion != nil {
		u.Object["allowVolumeExpansion"] = *allowExpansion
	}
	return u
}

func TestGetResourceNestedBool(t *testing.T) {
	yes, no := true, false
	c := newFakeClient(t, storageAPIResources(),
		newStorageClass("fast", &yes), newStorageClass("slow", &no), newStorageClass("unset", nil))

	cases := []struct {
		name      string
		wantValue bool
		wantFound bool
	}{
		{"fast", true, true},
		{"slow", false, true},
		{"unset", false, false},
		{"missing", false, false},
	}
	for _, tc := range cases {
		v, found, err := c.GetResourceNestedBool(context.Background(), "storage.k8s.io/v1", "StorageClass", "", tc.name, "allowVolumeExpansion")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if v != tc.wantValue || found != tc.wantFound {
			t.Fatalf("%s: got (%v, %v), want (%v, %v)", tc.name, v, found, tc.wantValue, tc.wantFound)
		}
	}
}

func TestResizePVC_SetsRequestedStorage(t *testing.T) {
	pvc := newOwnedPVC("ns", "data")
	_ = unstructured.SetNestedField(pvc.Object, "500Gi", "spec", "resources", "requests", "storage")
	_ = unstructured.SetNestedStringSlice(pvc.Object, []string{"ReadWriteOnce"}, "spec", "accessModes")
	c := newFakeClient(t, coreOnlyAPIResources(), pvc)

	if err := c.ResizePVC(context.Background(), "ns", "data", "1Ti"); err != nil {
		t.Fatalf("ResizePVC returned an error: %v", err)
	}

	gvr, _ := ToGroupVersionResource(KindPVC)
	obj, err := c.Dyn.Resource(gvr).Namespace("ns").Get(context.Background(), "data", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get PVC: %v", err)
	}
	size, _, _ := unstructured.NestedString(obj.Object, "spec", "resources", "requests", "storage")
	if size != "1Ti" {
		t.Fatalf("expected storage 1Ti, got %q", size)
	}
	modes, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "accessModes")
	if len(modes) != 1 || modes[0] != "ReadWriteOnce" {
		t.Fatalf("expected other spec fields to stay, got accessModes %v", modes)
	}
}

func TestResizePVC_MissingPVCIsError(t *testing.T) {
	c := newFakeClient(t, coreOnlyAPIResources())
	if err := c.ResizePVC(context.Background(), "ns", "missing", "1Ti"); err == nil {
		t.Fatalf("expected an error for a missing PVC")
	}
}
