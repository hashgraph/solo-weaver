// SPDX-License-Identifier: Apache-2.0

// Package cilium reads the Cilium deployment in kube-system through the
// Kubernetes API, so callers can decide whether to install Cilium and which
// chart version an existing deployment runs without executing the cilium CLI.
package cilium

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

const (
	Namespace = "kube-system"

	configMapName      = "cilium-config"
	daemonSetName      = "cilium"
	operatorName       = "cilium-operator"
	releaseName        = "cilium"
	agentContainerName = "cilium-agent"
)

var (
	configMapsGVR  = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	secretsGVR     = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
	daemonSetsGVR  = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}
	deploymentsGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}

	// releaseVersion accepts a plain release version such as 1.18.1 or
	// v1.20.0-rc.1, never a moving tag like "latest" or "stable".
	releaseVersion = regexp.MustCompile(`^v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$`)
)

// Installation lists the Cilium resources found in kube-system. Any one of them
// means Cilium is installed, even if it is incomplete or unhealthy.
type Installation struct {
	Resources []string
}

func (i Installation) Found() bool { return len(i.Resources) > 0 }

// DetectInstallation looks for the resources a Cilium install leaves in
// kube-system: the cilium-config ConfigMap, the agent DaemonSet, the operator
// Deployment and the Helm release. Any one found is enough, even if other
// lookups fail. With none found, a lookup error other than NotFound is
// returned rather than read as absence.
func DetectInstallation(ctx context.Context, dyn dynamic.Interface) (Installation, error) {
	var found Installation
	var lookupErr error
	for _, r := range []struct {
		gvr  schema.GroupVersionResource
		name string
	}{
		{configMapsGVR, configMapName},
		{daemonSetsGVR, daemonSetName},
		{deploymentsGVR, operatorName},
	} {
		_, err := dyn.Resource(r.gvr).Namespace(Namespace).Get(ctx, r.name, metav1.GetOptions{})
		switch {
		case err == nil:
			found.Resources = append(found.Resources, r.gvr.Resource+"/"+r.name)
		case !kerrors.IsNotFound(err) && lookupErr == nil:
			lookupErr = apiError(err, "failed to check for %s %s/%s", r.gvr.Resource, Namespace, r.name)
		}
	}

	releases, err := releaseSecrets(ctx, dyn, "")
	switch {
	case err == nil && len(releases) > 0:
		found.Resources = append(found.Resources, "helm-release/"+releaseName)
	case err != nil && lookupErr == nil:
		lookupErr = err
	}

	if found.Found() {
		if lookupErr != nil {
			logx.As().Warn().Err(lookupErr).Strs("resources", found.Resources).
				Msg("Some Cilium resources could not be checked; the ones found are enough to treat Cilium as installed")
		}
		return found, nil
	}
	if lookupErr != nil {
		return Installation{}, lookupErr
	}
	return found, nil
}

// InstalledVersion returns the chart version of the deployed Cilium, without a
// leading "v". The cilium-agent container image tag in the agent DaemonSet and
// the deployed Helm release must name the same release; anything missing,
// unparseable or inconsistent is an error, so the result is safe to pass to
// `cilium upgrade --version`.
func InstalledVersion(ctx context.Context, dyn dynamic.Interface) (string, error) {
	agent, err := agentVersion(ctx, dyn)
	if err != nil {
		return "", err
	}

	chart, err := deployedChartVersion(ctx, dyn)
	if err != nil {
		return "", err
	}

	if agent != chart {
		return "", versionError(errorx.IllegalState.New(
			"Cilium agent image is %s but the deployed Helm release is chart %s; refusing to pick one", agent, chart))
	}

	return chart, nil
}

func agentVersion(ctx context.Context, dyn dynamic.Interface) (string, error) {
	ds, err := dyn.Resource(daemonSetsGVR).Namespace(Namespace).Get(ctx, daemonSetName, metav1.GetOptions{})
	if err != nil {
		if kerrors.IsNotFound(err) {
			return "", versionError(errorx.IllegalState.New("Cilium agent DaemonSet %s/%s not found", Namespace, daemonSetName))
		}
		return "", apiError(err, "failed to read DaemonSet %s/%s", Namespace, daemonSetName)
	}

	containers, _, _ := unstructured.NestedSlice(ds.Object, "spec", "template", "spec", "containers")
	var images []string
	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok || container["name"] != agentContainerName {
			continue
		}
		if image, ok := container["image"].(string); ok {
			images = append(images, image)
		}
	}
	if len(images) != 1 {
		return "", versionError(errorx.IllegalState.New(
			"expected one %q container image in DaemonSet %s/%s, found %d", agentContainerName, Namespace, daemonSetName, len(images)))
	}

	version, ok := imageVersion(images[0])
	if !ok {
		return "", versionError(errorx.IllegalState.New(
			"cannot read a release version from Cilium agent image %q", images[0]))
	}
	return version, nil
}

// imageVersion extracts the release version from an image reference such as
// quay.io/cilium/cilium:v1.18.1@sha256:....
func imageVersion(image string) (string, bool) {
	ref, _, _ := strings.Cut(image, "@")
	i := strings.LastIndex(ref, ":")
	if i < 0 || strings.Contains(ref[i+1:], "/") {
		return "", false
	}
	return normalizeVersion(ref[i+1:])
}

func normalizeVersion(v string) (string, bool) {
	m := releaseVersion.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return "", false
	}
	return m[1], true
}

func deployedChartVersion(ctx context.Context, dyn dynamic.Interface) (string, error) {
	releases, err := releaseSecrets(ctx, dyn, "deployed")
	if err != nil {
		return "", err
	}
	if len(releases) != 1 {
		return "", versionError(errorx.IllegalState.New(
			"expected one deployed Helm release %q in %s, found %d", releaseName, Namespace, len(releases)))
	}

	encoded, _, _ := unstructured.NestedString(releases[0].Object, "data", "release")
	chart, err := chartVersionFromRelease(encoded)
	if err != nil {
		return "", versionError(errorx.IllegalFormat.Wrap(err, "cannot read Helm release secret %s/%s", Namespace, releases[0].GetName()))
	}

	version, ok := normalizeVersion(chart)
	if !ok {
		return "", versionError(errorx.IllegalState.New("deployed Helm release %q has invalid chart version %q", releaseName, chart))
	}
	return version, nil
}

// releaseSecrets lists the Helm storage secrets of the cilium release, filtered
// by Helm status when status is not empty.
func releaseSecrets(ctx context.Context, dyn dynamic.Interface, status string) ([]unstructured.Unstructured, error) {
	selector := "owner=helm,name=" + releaseName
	if status != "" {
		selector += ",status=" + status
	}
	list, err := dyn.Resource(secretsGVR).Namespace(Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, apiError(err, "failed to list Helm release secrets for %q in %s", releaseName, Namespace)
	}
	return list.Items, nil
}

// chartVersionFromRelease decodes a Helm release as stored in a secret's
// data.release field: base64 of the secret bytes, which hold Helm's own base64
// of a gzipped JSON release.
func chartVersionFromRelease(encoded string) (string, error) {
	stored, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "secret data is not base64")
	}
	payload, err := base64.StdEncoding.DecodeString(string(stored))
	if err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "release payload is not base64")
	}

	if bytes.HasPrefix(payload, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "release payload is not valid gzip")
		}
		defer func() { _ = zr.Close() }()
		if payload, err = io.ReadAll(zr); err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "release payload is not valid gzip")
		}
	}

	var rel struct {
		Chart struct {
			Metadata struct {
				Version string `json:"version"`
			} `json:"metadata"`
		} `json:"chart"`
	}
	if err := json.Unmarshal(payload, &rel); err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "release payload is not valid JSON")
	}
	return rel.Chart.Metadata.Version, nil
}

func apiError(err error, format string, args ...any) error {
	return errx.Decorate(errorx.ExternalError.Wrap(err, format, args...), reasons.K8sAPIFailed,
		"Check that the Kubernetes API server is up and the admin kubeconfig is valid: kubectl -n kube-system get configmap,daemonset,deployment",
		"Check that the provisioner's credentials can read kube-system: kubectl auth can-i get daemonsets -n kube-system")
}

func versionError(err error) error {
	return errx.Decorate(err, reasons.PreconditionNotMet,
		"Check the agent image: kubectl -n kube-system get daemonset cilium -o jsonpath='{.spec.template.spec.containers[?(@.name==\"cilium-agent\")].image}'",
		"Check the deployed chart: helm -n kube-system list --filter '^cilium$'")
}
