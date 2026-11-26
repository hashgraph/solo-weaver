// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"errors"
	"syscall"
	"testing"

	"github.com/joomcode/errorx"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// applyPatchOptions applies a ConfigMap with opts and returns the PatchOptions the server received.
func applyPatchOptions(t *testing.T, opts ...ApplyOption) metav1.PatchOptions {
	t.Helper()
	c := newFakeClient(t, coreOnlyAPIResources())
	var got metav1.PatchOptions
	c.Dyn.(*dynamicfake.FakeDynamicClient).PrependReactor("patch", "configmaps", func(a k8stesting.Action) (bool, runtime.Object, error) {
		got = a.(k8stesting.PatchActionImpl).PatchOptions
		return true, nil, nil
	})
	cm := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
	}
	if err := c.ApplyTyped(context.Background(), cm, opts...); err != nil {
		t.Fatalf("ApplyTyped returned an error: %v", err)
	}
	return got
}

func TestApplyTyped_DefaultSavesTheObject(t *testing.T) {
	got := applyPatchOptions(t)
	if len(got.DryRun) != 0 {
		t.Fatalf("expected no DryRun, got %v", got.DryRun)
	}
	if got.FieldManager != "solo-weaver" || got.Force == nil || !*got.Force {
		t.Fatalf("expected a forced apply by solo-weaver, got %+v", got)
	}
}

func TestApplyTyped_WithDryRunSavesNothing(t *testing.T) {
	got := applyPatchOptions(t, WithDryRun())
	if len(got.DryRun) != 1 || got.DryRun[0] != metav1.DryRunAll {
		t.Fatalf("expected DryRun=[All], got %v", got.DryRun)
	}
	if got.FieldManager != "solo-weaver" {
		t.Fatalf("expected the solo-weaver field manager, got %q", got.FieldManager)
	}
}

func TestIsTransientAPIError(t *testing.T) {
	gr := schema.GroupResource{Group: SoloOperatorGroup, Resource: "consensuscapsules"}
	cases := map[string]struct {
		err  error
		want bool
	}{
		"nil":                   {nil, false},
		"service unavailable":   {kerrors.NewServiceUnavailable("down"), true},
		"too many requests":     {kerrors.NewTooManyRequests("slow down", 1), true},
		"server timeout":        {kerrors.NewServerTimeout(gr, "patch", 1), true},
		"internal error":        {kerrors.NewInternalError(errors.New("etcd")), true},
		"connection refused":    {syscall.ECONNREFUSED, true},
		"wrapped by errorx":     {errorx.InternalError.Wrap(kerrors.NewServiceUnavailable("down"), "failed to apply"), true},
		"admission rejection":   {errorx.RejectedOperation.New("spec.orbit must equal the target namespace"), false},
		"forbidden":             {kerrors.NewForbidden(gr, "c", errors.New("denied")), false},
		"invalid":               {kerrors.NewInvalid(schema.GroupKind{Group: SoloOperatorGroup, Kind: "ConsensusCapsule"}, "c", nil), false},
		"wrapped 422 rejection": {errorx.InternalError.Wrap(kerrors.NewInvalid(schema.GroupKind{Group: SoloOperatorGroup, Kind: "ConsensusCapsule"}, "c", nil), "failed to apply"), false},
		"wrapped invalid":       {errorx.InternalError.Wrap(kerrors.NewBadRequest("bad"), "failed to apply"), false},
		"plain error":           {errors.New("boom"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := IsTransientAPIError(tc.err); got != tc.want {
				t.Errorf("IsTransientAPIError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
