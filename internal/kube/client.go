package kube

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	KindNamespace  ResourceKind = "Namespace"
	KindConfigMaps ResourceKind = "ConfigMap"
	KindPod        ResourceKind = "Pod"
	KindDeployment ResourceKind = "Deployment"
	KindJob        ResourceKind = "Job"
	KindPVC        ResourceKind = "PVC"

	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
	PhaseUnknown   Phase = "Unknown"
)

// ResourceKind represents a Kubernetes resource kind
type ResourceKind string

// String returns the string representation of the ResourceKind
func (r ResourceKind) String() string {
	return string(r)
}

// ToResourceKind converts a GroupVersionResource to ResourceKind
func ToResourceKind(gvr schema.GroupVersionResource) ResourceKind {
	switch gvr.Resource {
	case "namespaces":
		return KindNamespace
	case "configmaps":
		return KindConfigMaps
	case "pods":
		return KindPod
	case "deployments":
		return KindDeployment
	case "jobs":
		return KindJob
	case "persistentvolumeclaims":
		return KindPVC
	}

	return ResourceKind(gvr.Resource)
}

// ToGroupVersionResource converts a ResourceKind to GroupVersionResource
func ToGroupVersionResource(kind ResourceKind) (schema.GroupVersionResource, error) {
	switch kind {
	case KindNamespace:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}, nil
	case KindConfigMaps:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, nil
	case KindPod:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, nil
	case KindDeployment:
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, nil
	case KindJob:
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, nil
	case KindPVC:
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "persistentvolumeclaims"}, nil
	default:
		return schema.GroupVersionResource{}, errorx.IllegalArgument.New("unsupported resource kind: %s", kind)
	}
}

// Phase represents a Kubernetes resource phase/status
type Phase string

// String returns the string representation of the Phase
func (p Phase) String() string {
	return string(p)
}

// ToPhase converts a string to Phase
func ToPhase(s string) Phase {
	switch s {
	case "Pending":
		return PhasePending
	case "Running":
		return PhaseRunning
	case "Succeeded":
		return PhaseSucceeded
	case "Failed":
		return PhaseFailed
	case "Unknown":
		return PhaseUnknown
	default:
		return Phase(s)
	}
}

// CheckFunc defines a function type for checking resource conditions
type CheckFunc func(*unstructured.Unstructured) (bool, error)

// =====================
// Client
// =====================

// Client wraps Kubernetes dynamic client and REST mapper
// It is intended to be a replacement of invoking kubectl commands directly
type Client struct {
	Dyn    dynamic.Interface
	Mapper *restmapper.DeferredDiscoveryRESTMapper
}

// NewClient creates a new kube.Client using default kubeconfig rules
func NewClient() (*Client, error) {
	var config *rest.Config
	var err error
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		// Running inside cluster
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "failed to load in-cluster config")
		}
	} else {
		// Local dev / test
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "failed to load kubeconfig")
		}
	}

	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create dynamic client")
	}

	disco, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create discovery client")
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(disco))
	return &Client{
		Dyn:    dyn,
		Mapper: mapper,
	}, nil
}

// =====================
// Manifest Parsing
// =====================

// parseManifests reads and parses Kubernetes manifests from the given file path
func parseManifests(manifestPath string) ([]*unstructured.Unstructured, error) {
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "error reading manifest")
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
			return nil, errorx.IllegalFormat.Wrap(err, "error decoding yaml")
		}
		if len(u.Object) == 0 {
			continue
		}
		objs = append(objs, u)
	}
	return objs, nil
}

// =====================
// ResourceKind Processing
// =====================

// resourceHandler defines a function type for processing a resource
type resourceHandler func(ctx context.Context, mapping *meta.RESTMapping, dr dynamic.ResourceInterface, u *unstructured.Unstructured) error

// processResources processes a list of unstructured objects using the given handler function
func (c *Client) processResources(ctx context.Context, objs []*unstructured.Unstructured, handler resourceHandler) error {
	for _, u := range objs {
		gvk := schema.FromAPIVersionAndKind(u.GetAPIVersion(), u.GetKind())
		mapping, err := c.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return errorx.IllegalArgument.Wrap(err, "error in restmapping %s", gvk.String())
		}

		var dr dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			ns := u.GetNamespace()
			if ns == "" {
				ns = "default"
			}
			dr = c.Dyn.Resource(mapping.Resource).Namespace(ns)
		} else {
			dr = c.Dyn.Resource(mapping.Resource)
		}

		if err := handler(ctx, mapping, dr, u); err != nil {
			return err
		}
	}
	return nil
}

// =====================
// Apply / Delete Manifests
// =====================

// ApplyManifest applies resources defined in the given manifest file
func (c *Client) ApplyManifest(ctx context.Context, manifestPath string) error {
	objs, err := parseManifests(manifestPath)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, mapping *meta.RESTMapping, dr dynamic.ResourceInterface, u *unstructured.Unstructured) error {
		existing, err := dr.Get(ctx, u.GetName(), metav1.GetOptions{})
		if kerrors.IsNotFound(err) {
			if _, err := dr.Create(ctx, u, metav1.CreateOptions{}); err != nil {
				return errorx.IllegalArgument.Wrap(err, "error during create %s/%s", mapping.Resource.Resource, u.GetName())
			}
			return nil
		} else if err != nil {
			return errorx.IllegalArgument.Wrap(err, "error during get %s/%s", mapping.Resource.Resource, u.GetName())
		}

		u.SetResourceVersion(existing.GetResourceVersion())
		if _, err := dr.Update(ctx, u, metav1.UpdateOptions{}); err != nil {
			return errorx.InternalError.Wrap(err, "error during update %s/%s", mapping.Resource.Resource, u.GetName())
		}
		return nil
	}

	return c.processResources(ctx, objs, handler)
}

// DeleteManifest deletes resources defined in the given manifest file
func (c *Client) DeleteManifest(ctx context.Context, manifestPath string) error {
	objs, err := parseManifests(manifestPath)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, mapping *meta.RESTMapping, dr dynamic.ResourceInterface, u *unstructured.Unstructured) error {
		policy := metav1.DeletePropagationBackground
		if err := dr.Delete(ctx, u.GetName(), metav1.DeleteOptions{
			PropagationPolicy: &policy,
		}); err != nil && !kerrors.IsNotFound(err) {
			return errorx.InternalError.Wrap(err, "error during delete %s/%s", mapping.Resource.Resource, u.GetName())
		}
		return nil
	}

	return c.processResources(ctx, objs, handler)
}

// =====================
// Generic WaitFor
// =====================

type WaitOptions struct {
	NamePrefix string
	// A selector to restrict the list of returned objects by their labels.
	// Defaults to everything.
	// +optional
	LabelSelector string `json:"labelSelector,omitempty" protobuf:"bytes,1,opt,name=labelSelector"`
	// A selector to restrict the list of returned objects by their fields.
	// Defaults to everything.
	// +optional
	FieldSelector string `json:"fieldSelector,omitempty" protobuf:"bytes,2,opt,name=fieldSelector"`
}

func (w WaitOptions) AsListOptions() metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: w.LabelSelector,
		FieldSelector: w.FieldSelector,
	}
}

func (w WaitOptions) String() string {
	var parts []string
	if w.NamePrefix != "" {
		parts = append(parts, "namePrefix="+w.NamePrefix)
	}
	if w.LabelSelector != "" {
		parts = append(parts, "labelSelector="+w.LabelSelector)
	}
	if w.FieldSelector != "" {
		parts = append(parts, "fieldSelector="+w.FieldSelector)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (c *Client) List(ctx context.Context, gvr schema.GroupVersionResource, namespace string, opts WaitOptions) (*unstructured.UnstructuredList, error) {
	var dr dynamic.ResourceInterface
	if namespace != "" {
		dr = c.Dyn.Resource(gvr).Namespace(namespace)
	} else {
		dr = c.Dyn.Resource(gvr)
	}

	items, err := dr.List(ctx, opts.AsListOptions())
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "error during list %s in ns=%s: %s", gvr.Resource, namespace, opts.String())
	}

	if opts.NamePrefix != "" {
		filtered := &unstructured.UnstructuredList{}
		for _, item := range items.Items {
			if strings.HasPrefix(item.GetName(), opts.NamePrefix) {
				filtered.Items = append(filtered.Items, item)
			}
		}
		items = filtered
	}

	return items, nil
}

// WaitFor waits for a resource to satisfy the given check function within the timeout
func (c *Client) WaitFor(ctx context.Context, gvr schema.GroupVersionResource, namespace string,
	checkFn CheckFunc, timeout time.Duration, opts WaitOptions) error {

	deadline := time.Now().Add(timeout)
	for {
		items, err := c.List(ctx, gvr, namespace, opts)

		if err != nil {
			return err
		}

		total := len(items.Items)
		count := 0
		for _, obj := range items.Items {
			if err != nil {
				if kerrors.IsNotFound(err) {
					// keep waiting
				} else {
					return err
				}
			} else {
				ok, err := checkFn(obj.DeepCopy())
				if err != nil {
					return err
				}
				if ok {
					count++
				}
			}
		}

		// if total is zero, we keep waiting
		// otherwise, we wait until all items satisfy the condition
		if total > 0 && count == total {
			return nil
		}

		if time.Now().After(deadline) {
			return errorx.IllegalState.New("timed out waiting for %s: %s", namespace, opts.String())
		}

		if err = sleepWithContext(ctx, 300*time.Millisecond); err != nil {
			return err
		}
	}
}

// WaitForGet is a shared polling helper. predicate receives the result of Get (obj, err)
// and should return (done, error). When done == true the helper returns nil.
func (c *Client) waitForGet(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, timeout time.Duration,
	predicate func(obj *unstructured.Unstructured, getErr error) (bool, error)) error {

	var dr dynamic.ResourceInterface
	if namespace != "" {
		dr = c.Dyn.Resource(gvr).Namespace(namespace)
	} else {
		dr = c.Dyn.Resource(gvr)
	}

	deadline := time.Now().Add(timeout)
	poll := 300 * time.Millisecond

	for {
		obj, err := dr.Get(ctx, name, metav1.GetOptions{})
		done, perr := predicate(obj, err)
		if perr != nil {
			return perr
		}
		if done {
			return nil
		}

		if time.Now().After(deadline) {
			return errorx.IllegalState.Wrap(err, "timed out waiting for %s/%s in ns=%s", gvr.Resource, name, namespace)
		}

		if err := sleepWithContext(ctx, poll); err != nil {
			return err
		}
	}
}

// WaitForExistence waits until the specified resource can be Get()'d (exists) or times out.
func (c *Client) WaitForExistence(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, timeout time.Duration) error {
	return c.waitForGet(ctx, gvr, namespace, name, timeout, func(obj *unstructured.Unstructured, err error) (bool, error) {
		if err == nil {
			return true, nil
		}
		if kerrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})
}

// WaitForDeleted waits until Get() returns NotFound (deleted) or times out.
func (c *Client) WaitForDeleted(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, timeout time.Duration) error {
	return c.waitForGet(ctx, gvr, namespace, name, timeout, func(obj *unstructured.Unstructured, err error) (bool, error) {
		if err != nil {
			if kerrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		// object still exists -> keep waiting
		return false, nil
	})
}

// sleepWithContext sleeps for the given duration or returns early if the context is done
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

// =====================
// Generic WaitForResource by Kind
// =====================

// WaitForResource waits for a resource of the given kind to reach its ready/completed state
func (c *Client) WaitForResource(ctx context.Context, kind ResourceKind, namespace string, checkFn CheckFunc, timeout time.Duration, opts WaitOptions) error {
	var gvr schema.GroupVersionResource

	gvr, err := ToGroupVersionResource(kind)
	if err != nil {
		return err
	}

	return c.WaitFor(ctx, gvr, namespace, checkFn, timeout, opts)
}

// WaitForContainer waits until the specified container in the given Pod is ready
// or has terminated successfully within the timeout. It returns an error when the
// container terminates with a non-zero exit code or on other failures.
func (c *Client) WaitForContainer(ctx context.Context, namespace string, checkFn CheckFunc, timeout time.Duration, opts WaitOptions) error {
	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	return c.WaitFor(ctx, gvr, namespace, checkFn, timeout, opts)
}

// =====================
// Condition Functions
// =====================

// IsPodReady checks if a Pod is in Ready condition
func IsPodReady(obj *unstructured.Unstructured) (bool, error) {
	status, found, _ := unstructured.NestedMap(obj.Object, "status")
	if !found {
		return false, nil
	}

	phase, _, _ := unstructured.NestedString(status, "phase")
	if phase == "Failed" {
		return false, errorx.IllegalState.New("pod %q failed", obj.GetName())
	}
	if phase != "Running" {
		return false, nil
	}

	conditions, found, _ := unstructured.NestedSlice(status, "conditions")
	if !found {
		return false, nil
	}

	for _, c := range conditions {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		if m["type"] == "Ready" && m["status"] == "True" {
			return true, nil
		}
	}
	return false, nil
}

// IsDeploymentReady checks if a Deployment has all desired replicas ready
func IsDeploymentReady(obj *unstructured.Unstructured) (bool, error) {
	status, found, _ := unstructured.NestedMap(obj.Object, "status")
	if !found {
		return false, nil
	}

	desired, _, _ := unstructured.NestedInt64(status, "replicas")
	ready, _, _ := unstructured.NestedInt64(status, "readyReplicas")

	if desired > 0 && ready == desired {
		return true, nil
	}
	return false, nil
}

// IsJobComplete checks if a Job has completed successfully
func IsJobComplete(obj *unstructured.Unstructured) (bool, error) {
	conds, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return false, nil
	}

	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		if m["type"] == "Complete" && m["status"] == "True" {
			return true, nil
		}
		if m["type"] == "Failed" && m["status"] == "True" {
			return false, errorx.IllegalState.New("job %q failed", obj.GetName())
		}
	}
	return false, nil
}

// IsPVCBound checks if a PersistentVolumeClaim is in Bound phase
func IsPVCBound(obj *unstructured.Unstructured) (bool, error) {
	phase, found, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if !found {
		return false, nil
	}
	return phase == "Bound", nil
}

// IsPhase returns a check function that verifies if the resource is in the desired phase
func IsPhase(desired Phase) func(*unstructured.Unstructured) (bool, error) {
	return func(obj *unstructured.Unstructured) (bool, error) {
		phase, found, _ := unstructured.NestedString(obj.Object, "status", "phase")
		if !found {
			return false, nil
		}
		return phase == desired.String(), nil
	}
}

// IsContainerReady returns a CheckFunc that succeeds when the named container reports Ready==true.
// This is meant to be used for WaitForContainer function.
func IsContainerReady(containerName string) CheckFunc {
	return func(obj *unstructured.Unstructured) (bool, error) {
		cs, found, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")
		if !found {
			return false, nil
		}

		for _, it := range cs {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}

			name, _, _ := unstructured.NestedString(m, "name")
			if name != containerName {
				continue
			}
			ready, foundReady, _ := unstructured.NestedBool(m, "ready")
			if foundReady && ready {
				return true, nil
			}
		}

		return false, nil
	}
}

// IsContainerTerminated returns a CheckFunc that succeeds when the named container terminated with the specified exit code.
// If terminated with a different exit code it returns an error.
// This is meant to be used for WaitForContainer function.
func IsContainerTerminated(containerName string, wantCode int64) CheckFunc {
	return func(obj *unstructured.Unstructured) (bool, error) {
		cs, found, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")
		if !found {
			return false, nil
		}
		for _, it := range cs {
			m, ok := it.(map[string]interface{})
			if !ok {
				continue
			}

			name, _, _ := unstructured.NestedString(m, "name")
			if name != containerName {
				continue
			}
			code, foundTerm, _ := unstructured.NestedInt64(m, "state", "terminated", "exitCode")
			if !foundTerm {
				return false, nil
			}
			if code == wantCode {
				return true, nil
			}
			return false, errorx.IllegalState.New("container %q terminated with exit code %d (want %d)", containerName, code, wantCode)
		}
		return false, nil
	}
}
