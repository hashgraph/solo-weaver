package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/joomcode/errorx"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// buildConfigAndClient builds kube config following default rules and instantiates a dynamic client and REST mapper.
func buildConfigAndClient() (*restmapper.DeferredDiscoveryRESTMapper, *dynamic.DynamicClient, error) {
	kubeconfig := "" // default loading rules
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, nil, errorx.IllegalArgument.New("build kubeconfig: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, nil, errorx.InternalError.New("create dynamic client: %w", err)
	}

	discoClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, nil, errorx.InternalError.New("create discovery client: %w", err)
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoClient))
	return mapper, dynClient, nil
}

// parseManifests reads a file and returns decoded non-empty Unstructured documents.
func parseManifests(manifestPath string) ([]*unstructured.Unstructured, error) {
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, errorx.IllegalArgument.New("read manifest: %w", err)
	}

	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), 4096)
	var objs []*unstructured.Unstructured
	for {
		u := &unstructured.Unstructured{}
		if err := dec.Decode(&u.Object); err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				continue
			}
			return nil, errorx.IllegalFormat.New("decode yaml: %w", err)
		}
		if len(u.Object) == 0 {
			continue
		}
		objs = append(objs, u)
	}
	return objs, nil
}

// resourceHandler is called for each decoded resource.
type resourceHandler func(ctx context.Context, mapping *meta.RESTMapping, dr dynamic.ResourceInterface, u *unstructured.Unstructured) error

// processResources resolves mappings and invokes handler for each resource.
func processResources(ctx context.Context, objs []*unstructured.Unstructured, mapper *restmapper.DeferredDiscoveryRESTMapper, dyn dynamic.Interface, handler resourceHandler) error {
	for _, u := range objs {
		gvk := schema.FromAPIVersionAndKind(u.GetAPIVersion(), u.GetKind())
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return errorx.IllegalFormat.New("restmapping %s: %w", gvk.String(), err)
		}

		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns := u.GetNamespace()
			if ns == "" {
				ns = "default"
			}
			dr = dyn.Resource(mapping.Resource).Namespace(ns)
		} else {
			dr = dyn.Resource(mapping.Resource)
		}

		if err := handler(ctx, mapping, dr, u); err != nil {
			return err
		}
	}
	return nil
}

func waitForResourcePresence(ctx context.Context, dyn *dynamic.DynamicClient, gvr schema.GroupVersionResource, name string, wantPresent bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		// use the dynamic interface returned by buildConfigAndClient which is *dynamic.DynamicClient
		dr := dyn.Resource(gvr)
		_, err := dr.Get(ctx, name, metav1.GetOptions{})
		if wantPresent {
			if err == nil {
				return nil
			}
			if kerrors.IsNotFound(err) {
				// keep waiting
			} else {
				return err
			}
		} else {
			if err != nil {
				if kerrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			// still present, keep waiting
		}
		if time.Now().After(deadline) {
			if wantPresent {
				return fmt.Errorf("timed out waiting for resource %s to appear", name)
			}
			return fmt.Errorf("timed out waiting for resource %s to be deleted", name)
		}

		err = sleepWithContext(ctx, 300*time.Millisecond)
		if err != nil {
			return err
		}
	}
}

// sleep sleeps for the given duration or returns early if the context
// is canceled or its deadline expires. Returns nil on success or ctx.Err() on cancellation.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ApplyManifest reads a YAML file (possibly multi-document) and creates or updates
// each resource using the dynamic client and REST mapper.
func ApplyManifest(ctx context.Context, manifestPath string) error {
	mapper, dynClient, err := buildConfigAndClient()
	if err != nil {
		return err
	}

	objs, err := parseManifests(manifestPath)
	if err != nil {
		return err
	}

	applyHandler := func(ctx context.Context, mapping *meta.RESTMapping, dr dynamic.ResourceInterface, u *unstructured.Unstructured) error {
		existing, err := dr.Get(ctx, u.GetName(), metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			if _, err := dr.Create(ctx, u, metav1.CreateOptions{}); err != nil {
				return errorx.InternalError.New("create %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
			}
			return nil
		} else if err != nil {
			return errorx.InternalError.New("get %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
		}

		u.SetResourceVersion(existing.GetResourceVersion())
		if _, err := dr.Update(ctx, u, metav1.UpdateOptions{}); err != nil {
			return errorx.InternalError.New("update %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
		}
		return nil
	}

	return processResources(ctx, objs, mapper, dynClient, applyHandler)
}

// DeleteManifest reads a YAML file (possibly multi-document) and deletes
// each resource using the dynamic client and REST mapper.
func DeleteManifest(ctx context.Context, manifestPath string) error {
	mapper, dynClient, err := buildConfigAndClient()
	if err != nil {
		return err
	}

	objs, err := parseManifests(manifestPath)
	if err != nil {
		return err
	}

	deleteHandler := func(ctx context.Context, mapping *meta.RESTMapping, dr dynamic.ResourceInterface, u *unstructured.Unstructured) error {
		policy := metav1.DeletePropagationBackground
		if err := dr.Delete(ctx, u.GetName(), metav1.DeleteOptions{
			PropagationPolicy: &policy,
		}); err != nil {
			if kerrors.IsNotFound(err) {
				return nil
			}
			return errorx.InternalError.New("delete %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
		}
		return nil
	}

	return processResources(ctx, objs, mapper, dynClient, deleteHandler)
}
