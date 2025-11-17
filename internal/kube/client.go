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
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

type ResourceKind string

func (k ResourceKind) String() string { return string(k) }

const (
	KindNode       ResourceKind = "Node"
	KindService    ResourceKind = "Service"
	KindNamespace  ResourceKind = "Namespace"
	KindConfigMap  ResourceKind = "ConfigMap"
	KindPod        ResourceKind = "Pod"
	KindDeployment ResourceKind = "Deployment"
	KindJob        ResourceKind = "Job"
	KindPVC        ResourceKind = "PersistentVolumeClaim"
	KindCRD        ResourceKind = "CustomResourceDefinition"
)

var kindToGVR = map[ResourceKind]schema.GroupVersionResource{
	KindNode:       {Group: "", Version: "v1", Resource: "nodes"},
	KindService:    {Group: "", Version: "v1", Resource: "services"},
	KindNamespace:  {Group: "", Version: "v1", Resource: "namespaces"},
	KindConfigMap:  {Group: "", Version: "v1", Resource: "configmaps"},
	KindPod:        {Group: "", Version: "v1", Resource: "pods"},
	KindDeployment: {Group: "apps", Version: "v1", Resource: "deployments"},
	KindJob:        {Group: "batch", Version: "v1", Resource: "jobs"},
	KindPVC:        {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
	KindCRD:        {Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"},
}

func RegisterKind(kind ResourceKind, gvr schema.GroupVersionResource) {
	kindToGVR[kind] = gvr
	key := gvr.Group + "/" + gvr.Version + "/" + gvr.Resource
	gvrToKind[key] = kind
}

var gvrToKind = map[string]ResourceKind{}

func init() {
	for k, v := range kindToGVR {
		key := v.Group + "/" + v.Version + "/" + v.Resource
		gvrToKind[key] = k
	}
}

func ToGroupVersionResource(kind ResourceKind) (schema.GroupVersionResource, error) {
	gvr, ok := kindToGVR[kind]
	if !ok {
		return schema.GroupVersionResource{}, errorx.IllegalArgument.New("unsupported resource kind: %s", kind)
	}
	return gvr, nil
}

func ToResourceKind(gvr schema.GroupVersionResource) ResourceKind {
	key := gvr.Group + "/" + gvr.Version + "/" + gvr.Resource
	if kind, ok := gvrToKind[key]; ok {
		return kind
	}
	return ResourceKind(gvr.Resource) // fallback
}

type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
	PhaseUnknown   Phase = "Unknown"
)

var knownPhases = map[string]Phase{
	"Pending":   PhasePending,
	"Running":   PhaseRunning,
	"Succeeded": PhaseSucceeded,
	"Failed":    PhaseFailed,
	"Unknown":   PhaseUnknown,
}

func (p Phase) String() string {
	return string(p)
}

// ToPhase converts a string to a Phase, normalizing the case
func ToPhase(s string) Phase {
	sn := cases.Title(language.Und, cases.NoLower).String(strings.ToLower(strings.TrimSpace(s)))
	if p, ok := knownPhases[sn]; ok {
		return p
	}
	return Phase(s)
}

// RegisterPhase allows registering a new Phase value
func RegisterPhase(name string) Phase {
	p := Phase(name)
	knownPhases[name] = p
	return p
}

// CheckFunc defines a function type for checking resource conditions
// Notes: when err != nil, obj may be nil.
// CheckFunc should handle API errors like IsNotFound, IsForbidden.
type CheckFunc func(obj *unstructured.Unstructured, err error) (bool, error)

// =====================
// Client
// =====================

// Client wraps Kubernetes dynamic client and REST mapper
// It is intended to be a replacement of invoking kubectl commands directly
type Client struct {
	Dyn    dynamic.Interface
	Mapper *restmapper.DeferredDiscoveryRESTMapper
}

// ClientProvider is a function that provides a kube client instance.
// NewClient is such a provider for general use.
// However, this is to help abstract the client creation and to allow better mocking and reusability of an instance of kube Client.
type ClientProvider func() (*Client, error)

// NewClient creates a Kubernetes client that automatically detects
// whether it is running inside a cluster or using a kubeconfig file.
// It returns a fully prepared dynamic client + discovery mapper.
func NewClient() (*Client, error) {
	config, err := loadKubeConfig()
	if err != nil {
		return nil, err
	}

	// Create dynamic client
	dyn, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create dynamic client")
	}

	// Create discovery client
	disco, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create discovery client")
	}

	// Create RESTMapper (auto-refreshing)
	memcache := memory.NewMemCacheClient(disco)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memcache)

	// Warm discovery cache to avoid first-call latency
	_, _ = mapper.ResourcesFor(schema.GroupVersionResource{Resource: "pods"})

	return &Client{
		Dyn:    dyn,
		Mapper: mapper,
	}, nil
}

// ---------------------------------------------------------------------
// loadKubeConfig detects in-cluster configuration or falls back
// to a kubeconfig file. Supports multi-KUBECONFIG paths.
// ---------------------------------------------------------------------
func loadKubeConfig() (*rest.Config, error) {
	// Detect in-cluster
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "failed to load in-cluster config")
		}
		return cfg, nil
	}

	// Detect KUBECONFIG (supports : separated multi-path)
	kubeconfigEnv := os.Getenv("KUBECONFIG")
	if kubeconfigEnv != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigEnv)
		if err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "failed to load kubeconfig from $KUBECONFIG")
		}
		return cfg, nil
	}

	// Default ~/.kube/config
	defaultPath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	cfg, err := clientcmd.BuildConfigFromFlags("", defaultPath)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err,
			"failed to load default kubeconfig at %s", defaultPath)
	}
	return cfg, nil
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
	// NamePrefix restricts the list of returned objects to those whose names start with the given prefix.
	// +optional
	NamePrefix string `json:"namePrefix,omitempty"`
	// A selector to restrict the list of returned objects by their labels.
	// Defaults to everything.
	// +optional
	LabelSelector string `json:"labelSelector,omitempty"`
	// A selector to restrict the list of returned objects by their fields.
	// Defaults to everything.
	// +optional
	FieldSelector string `json:"fieldSelector,omitempty"`
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

// List lists resources of the given kind in the specified namespace with optional filtering.
// If opts.NamePrefix is set, client-side filtering is applied to return only resources
// whose names start with the specified prefix.
func (c *Client) List(ctx context.Context, kind ResourceKind, namespace string, opts WaitOptions) (*unstructured.UnstructuredList, error) {
	gvr, err := ToGroupVersionResource(kind)
	if err != nil {
		return nil, err
	}

	var dr dynamic.ResourceInterface
	if namespace != "" {
		dr = c.Dyn.Resource(gvr).Namespace(namespace)
	} else {
		dr = c.Dyn.Resource(gvr)
	}

	list, err := dr.List(ctx, opts.AsListOptions())
	if err != nil {
		return nil, errorx.InternalError.Wrap(
			err,
			"list failed for %s (namespace=%s, opts=%s)",
			gvr.Resource, namespace, opts.String(),
		)
	}

	// No prefix filtering required
	if opts.NamePrefix == "" {
		return list, nil
	}

	// Client-side filtering by prefix
	filtered := &unstructured.UnstructuredList{
		Items: make([]unstructured.Unstructured, 0, len(list.Items)),
	}

	// Preserve metadata
	filtered.SetGroupVersionKind(list.GroupVersionKind())
	filtered.SetResourceVersion(list.GetResourceVersion())
	filtered.SetContinue(list.GetContinue())

	for _, item := range list.Items {
		if strings.HasPrefix(item.GetName(), opts.NamePrefix) {
			filtered.Items = append(filtered.Items, item)
		}
	}

	return filtered, nil
}

// WaitForResources waits until all resources of the given kind in the specified namespace
// satisfy the condition defined by checkFn within the timeout.
// It returns an error if the timeout is reached or if any fatal API errors occur.
func (c *Client) WaitForResources(
	ctx context.Context,
	kind ResourceKind,
	namespace string,
	checkFn CheckFunc,
	timeout time.Duration,
	opts WaitOptions,
) error {

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errorx.IllegalState.New(
				"timed out waiting for %s in ns=%s with opts=%s",
				kind, namespace, opts.String(),
			)

		case <-ticker.C:
			items, err := c.List(ctx, kind, namespace, opts)
			if err != nil {
				if kerrors.IsNotFound(err) {
					// Resource not created yet → keep waiting
					continue
				}

				if isFatalAPIError(err) {
					return errorx.InternalError.Wrap(err, "fatal API error listing for %s in ns=%s", kind, namespace)
				}

				return errorx.InternalError.Wrap(
					err, "list failed while waiting for %s in ns=%s",
					kind, namespace,
				)
			}

			if len(items.Items) == 0 {
				// No items matched → keep waiting
				continue
			}

			// All or nothing: every resource must satisfy condition
			allReady := true
			for _, item := range items.Items {
				ok, err := checkFn(item.DeepCopy(), nil)
				if err != nil {
					return err
				}
				if !ok {
					allReady = false
					break
				}
			}

			if allReady {
				return nil
			}
		}
	}
}

// WaitForResource waits until the specified resource of the given kind in the specified namespace
// satisfies the condition defined by checkFn within the timeout.
// It returns an error if the timeout is reached or if any fatal API errors occur.
func (c *Client) WaitForResource(
	ctx context.Context,
	kind ResourceKind,
	namespace, name string,
	checkFn CheckFunc,
	timeout time.Duration,
) error {

	gvr, err := ToGroupVersionResource(kind)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var dr dynamic.ResourceInterface
	if namespace != "" {
		dr = c.Dyn.Resource(gvr).Namespace(namespace)
	} else {
		dr = c.Dyn.Resource(gvr)
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errorx.IllegalState.Wrap(
				ctx.Err(),
				"timeout waiting for %s/%s in namespace=%s",
				gvr.Resource, name, namespace,
			)

		case <-ticker.C:
			obj, err := dr.Get(ctx, name, metav1.GetOptions{})
			if err != nil && isFatalAPIError(err) {
				return errorx.InternalError.Wrap(err, "fatal API error waiting for %s/%s", gvr.Resource, name)
			}

			// DeepCopy for safety
			if obj != nil {
				obj = obj.DeepCopy()
			}

			// Delegate handling of errors and readiness to checkFn.
			done, checkErr := checkFn(obj, err)
			if checkErr != nil {
				return checkErr
			}

			if done {
				return nil
			}
		}
	}
}

func isFatalAPIError(err error) bool {
	return kerrors.IsForbidden(err) ||
		kerrors.IsUnauthorized(err) ||
		kerrors.IsInvalid(err) ||
		kerrors.IsBadRequest(err)
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

// WaitForContainer waits until the specified container in the given Pod is ready
// or has terminated successfully within the timeout. It returns an error when the
// container terminates with a non-zero exit code or on other failures.
func (c *Client) WaitForContainer(ctx context.Context, namespace string, checkFn CheckFunc, timeout time.Duration, opts WaitOptions) error {
	return c.WaitForResources(ctx, KindPod, namespace, checkFn, timeout, opts)
}

// =====================
// Condition Functions
// =====================

// IsPresent checks if the item exists
func IsPresent(obj *unstructured.Unstructured, err error) (bool, error) {
	if err == nil {
		return true, nil
	}

	if kerrors.IsNotFound(err) {
		return false, nil
	}

	return false, err
}

// IsDeleted checks if a resource has been deleted (returns true if obj is nil or error is NotFound)
func IsDeleted(obj *unstructured.Unstructured, err error) (bool, error) {
	// If there's an error, check if it's NotFound
	if err != nil {
		if kerrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	// If no error, the object should exist (not deleted)
	if obj != nil {
		return false, nil
	}

	// obj is nil but no error - inconsistent state
	return false, errorx.IllegalState.New("resource returned nil object with no error")
}

// IsNodeReady checks if a Node has Ready condition == True
func IsNodeReady(obj *unstructured.Unstructured, err error) (bool, error) {
	if ok, err := IsPresent(obj, err); !ok || err != nil {
		return ok, err
	}

	conds, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return false, nil
	}

	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if m["type"] == "Ready" {
			if m["status"] == "True" {
				return true, nil
			}
			return false, nil
		}
	}
	return false, nil
}

// IsPodReady checks if a Pod is in Ready condition
func IsPodReady(obj *unstructured.Unstructured, err error) (bool, error) {
	if ok, err := IsPresent(obj, err); !ok || err != nil {
		return ok, err
	}

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
func IsDeploymentReady(obj *unstructured.Unstructured, err error) (bool, error) {
	if ok, err := IsPresent(obj, err); !ok || err != nil {
		return ok, err
	}

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
func IsJobComplete(obj *unstructured.Unstructured, err error) (bool, error) {
	if ok, err := IsPresent(obj, err); !ok || err != nil {
		return ok, err
	}

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
func IsPVCBound(obj *unstructured.Unstructured, err error) (bool, error) {
	if ok, err := IsPresent(obj, err); !ok || err != nil {
		return ok, err
	}

	phase, found, _ := unstructured.NestedString(obj.Object, "status", "phase")
	if !found {
		return false, nil
	}
	return phase == "Bound", nil
}

// IsPhase returns a check function that verifies if the resource is in the desired phase
func IsPhase(desired Phase) func(obj *unstructured.Unstructured, err error) (bool, error) {
	return func(obj *unstructured.Unstructured, err error) (bool, error) {
		if ok, err := IsPresent(obj, err); !ok || err != nil {
			return ok, err
		}

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
	return func(obj *unstructured.Unstructured, err error) (bool, error) {
		if ok, err := IsPresent(obj, err); !ok || err != nil {
			return ok, err
		}

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
	return func(obj *unstructured.Unstructured, err error) (bool, error) {
		if ok, err := IsPresent(obj, err); !ok || err != nil {
			return ok, err
		}

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

// IsCRDReady checks if a CustomResourceDefinition is established
func IsCRDReady(obj *unstructured.Unstructured, err error) (bool, error) {
	if ok, err := IsPresent(obj, err); !ok || err != nil {
		return ok, err
	}

	conds, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return false, nil
	}

	var established bool
	for _, c := range conds {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		// name acceptance failure should be surfaced as an error
		if m["type"] == "NamesAccepted" && m["status"] == "False" {
			reason, _ := m["reason"].(string)
			msg, _ := m["message"].(string)
			if reason == "" && msg == "" {
				return false, errorx.IllegalState.New("crd %q names not accepted", obj.GetName())
			}
			return false, errorx.IllegalState.New("crd %q names not accepted: %s %s", obj.GetName(), reason, msg)
		}

		if m["type"] == "Established" && m["status"] == "True" {
			established = true
		}
	}

	return established, nil
}
