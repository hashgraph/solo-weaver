package kube

import (
	"bytes"
	"context"
	"io"
	"os"

	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/api/errors"
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

// ApplyManifest reads a YAML file (possibly multi-document) and creates or updates
// each resource using the dynamic client and REST mapper.
func ApplyManifest(ctx context.Context, manifestPath string) error {
	kubeconfig := "" // follow default loading rules (KUBECONFIG env or ~/.kube/config)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return errorx.IllegalArgument.New("build kubeconfig: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return errorx.InternalError.New("create dynamic client: %w", err)
	}

	discoClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return errorx.InternalError.New("create discovery client: %w", err)
	}

	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoClient))

	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return errorx.IllegalArgument.New("read manifest: %w", err)
	}

	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(b), 4096)
	for {
		u := &unstructured.Unstructured{}
		if err := dec.Decode(&u.Object); err != nil {
			if err == io.EOF {
				break
			}
			// skip empty documents
			if err == io.ErrUnexpectedEOF {
				continue
			}
			return errorx.IllegalFormat.New("decode yaml: %w", err)
		}
		if len(u.Object) == 0 {
			continue
		}

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
			dr = dynClient.Resource(mapping.Resource).Namespace(ns)
		} else {
			dr = dynClient.Resource(mapping.Resource)
		}

		// attempt to get existing resource
		existing, err := dr.Get(ctx, u.GetName(), metav1.GetOptions{})
		if errors.IsNotFound(err) {
			if _, err := dr.Create(ctx, u, metav1.CreateOptions{}); err != nil {
				return errorx.InternalError.New("create %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
			}
		} else if err != nil {
			return errorx.InternalError.New("get %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
		} else {
			// preserve the existing resourceVersion to allow update
			u.SetResourceVersion(existing.GetResourceVersion())
			if _, err := dr.Update(ctx, u, metav1.UpdateOptions{}); err != nil {
				return errorx.InternalError.New("update %s/%s: %w", mapping.Resource.Resource, u.GetName(), err)
			}
		}
	}

	return nil
}
