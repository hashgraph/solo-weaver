// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"encoding/json"

	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	SoloOperatorGroup   = "operator.solo.hiero.org"
	SoloOperatorVersion = "v1alpha1"
)

const (
	KindOrbit            ResourceKind = "Orbit"
	KindConsensusCapsule ResourceKind = "ConsensusCapsule"
	KindNetworkGenesis   ResourceKind = "NetworkGenesis"
	KindHAProxyCapsule   ResourceKind = "HAProxyCapsule"
	KindEnvoyProxy       ResourceKind = "EnvoyProxy"
)

func init() {
	RegisterKind(KindOrbit, schema.GroupVersionResource{
		Group: SoloOperatorGroup, Version: SoloOperatorVersion, Resource: "orbits",
	})
	RegisterKind(KindConsensusCapsule, schema.GroupVersionResource{
		Group: SoloOperatorGroup, Version: SoloOperatorVersion, Resource: "consensuscapsules",
	})
	RegisterKind(KindNetworkGenesis, schema.GroupVersionResource{
		Group: SoloOperatorGroup, Version: SoloOperatorVersion, Resource: "networkgeneses",
	})
	RegisterKind(KindHAProxyCapsule, schema.GroupVersionResource{
		Group: SoloOperatorGroup, Version: SoloOperatorVersion, Resource: "haproxycapsules",
	})
	RegisterKind(KindEnvoyProxy, schema.GroupVersionResource{
		Group: SoloOperatorGroup, Version: SoloOperatorVersion, Resource: "envoyproxies",
	})
}

// ApplyTyped converts a typed Kubernetes object to unstructured and applies it
// via Server-Side Apply. The object must have TypeMeta set (APIVersion + Kind).
func (c *Client) ApplyTyped(ctx context.Context, obj runtime.Object) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal typed object")
	}

	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, &u.Object); err != nil {
		return errorx.InternalError.Wrap(err, "failed to unmarshal to unstructured")
	}

	gvk := u.GroupVersionKind()
	if gvk.Empty() {
		return errorx.IllegalArgument.New("object has no GVK set; ensure TypeMeta is populated")
	}

	mapping, err := c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "failed to get REST mapping for %s", gvk.String())
	}

	ns := u.GetNamespace()
	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if ns == "" {
			ns = "default"
		}
		dr = c.Dyn.Resource(mapping.Resource).Namespace(ns)
	} else {
		dr = c.Dyn.Resource(mapping.Resource)
	}

	force := true
	if _, err := dr.Patch(ctx, u.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "solo-weaver",
		Force:        &force,
	}); err != nil {
		return errorx.InternalError.Wrap(err, "failed to apply %s/%s", gvk.Kind, u.GetName())
	}

	return nil
}
