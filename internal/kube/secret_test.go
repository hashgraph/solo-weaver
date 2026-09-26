// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"encoding/base64"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func secretAPIResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "secrets", SingularName: "secret", Namespaced: true, Kind: "Secret"},
			},
		},
	}
}

func seededSecret(namespace, name string, data map[string]string) *unstructured.Unstructured {
	encoded := map[string]interface{}{}
	for k, v := range data {
		encoded[k] = base64.StdEncoding.EncodeToString([]byte(v))
	}
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]interface{}{"namespace": namespace, "name": name},
		"type":       "Opaque",
		"data":       encoded,
	}}
	return u
}

func TestGetSecretData(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(t, secretAPIResources(), seededSecret("ns", "s1", map[string]string{"a": "alpha", "b": "beta"}))

	data, found, err := c.GetSecretData(ctx, "ns", "s1")
	if err != nil {
		t.Fatalf("GetSecretData error: %v", err)
	}
	if !found {
		t.Fatalf("expected secret to be found")
	}
	if string(data["a"]) != "alpha" || string(data["b"]) != "beta" {
		t.Fatalf("unexpected decoded data: %q / %q", data["a"], data["b"])
	}
}

func TestGetSecretData_NotFound(t *testing.T) {
	ctx := context.Background()
	c := newFakeClient(t, secretAPIResources())

	_, found, err := c.GetSecretData(ctx, "ns", "missing")
	if err != nil {
		t.Fatalf("GetSecretData error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for a missing secret")
	}
}

// ApplySecret uses Server-Side Apply (ApplyPatchType), which the fake dynamic
// client cannot create objects through, so its happy path is covered by the
// import/export orchestration tests (internal/consensus/keys) and by UAT against
// a real cluster rather than here.
