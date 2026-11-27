// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func capsuleAPIResources() []*metav1.APIResourceList {
	return []*metav1.APIResourceList{{
		GroupVersion: SoloOperatorAPIVersion,
		APIResources: []metav1.APIResource{
			{Name: "consensuscapsules", SingularName: "consensuscapsule", Namespaced: true, Kind: string(KindConsensusCapsule)},
		},
	}}
}

// The fake client cannot run SSA, so a reactor records the request instead.
func TestApplyFieldsAs_SendsApplyWithManagerAndForce(t *testing.T) {
	c := newFakeClient(t, capsuleAPIResources())
	dyn := c.Dyn.(*dynamicfake.FakeDynamicClient)

	var got k8stesting.PatchActionImpl
	dyn.PrependReactor("patch", "consensuscapsules", func(a k8stesting.Action) (bool, runtime.Object, error) {
		got = a.(k8stesting.PatchActionImpl)
		return true, &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": SoloOperatorAPIVersion, "kind": string(KindConsensusCapsule),
			"metadata": map[string]any{"name": got.Name, "namespace": got.Namespace, "generation": int64(7)},
		}}, nil
	})

	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": SoloOperatorAPIVersion,
		"kind":       string(KindConsensusCapsule),
		"metadata":   map[string]any{"name": "ns-consensus-0", "namespace": "ns"},
		"spec":       map[string]any{"desiredRunState": "Stopped"},
	}}
	live, err := c.ApplyFieldsAs(context.Background(), "solo-weaver-runstate", obj)
	require.NoError(t, err)
	require.Equal(t, int64(7), live.GetGeneration())

	require.Equal(t, types.ApplyPatchType, got.PatchType)
	require.Equal(t, "ns", got.Namespace)
	require.Equal(t, "ns-consensus-0", got.Name)
	require.Equal(t, "solo-weaver-runstate", got.PatchOptions.FieldManager)
	require.NotNil(t, got.PatchOptions.Force)
	require.True(t, *got.PatchOptions.Force)

	var body map[string]any
	require.NoError(t, json.Unmarshal(got.Patch, &body))
	require.Equal(t, map[string]any{"desiredRunState": "Stopped"}, body["spec"])
}

func TestApplyFieldsAs_RequiresManagerAndGVK(t *testing.T) {
	c := newFakeClient(t, capsuleAPIResources())
	_, err := c.ApplyFieldsAs(context.Background(), "", &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": SoloOperatorAPIVersion, "kind": string(KindConsensusCapsule),
	}})
	require.Error(t, err)

	_, err = c.ApplyFieldsAs(context.Background(), "m", &unstructured.Unstructured{Object: map[string]any{}})
	require.Error(t, err)
}
