// SPDX-License-Identifier: Apache-2.0

// Package probes holds the Kubernetes-specific leaf probe(s) that cannot live in
// the lightweight pkg/daemonkit because they pull in k8s.io/client-go. The
// generic, dependency-light probes (disk, composite, tagged) live in
// pkg/daemonkit and satisfy daemonkit.Probe, so they compose freely with
// KubeRBACProbe inside a daemonkit.CompositeProbe.
package probes

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	kubeRBACRetryInterval = 2 * time.Second
	kubeRBACRESTTimeout   = 30 * time.Second
)

// KubeRBACProbe verifies that the daemon's ServiceAccount has the specified
// verbs on a single Kubernetes group/resource in the given namespace. It
// retries every kubeRBACRetryInterval until all verbs are allowed or ctx is
// cancelled.
//
// One KubeRBACProbe covers one resource. If a monitor requires access to
// multiple resources, compose multiple KubeRBACProbe instances inside a
// daemon.CompositeProbe returned from RequiredProbe().
type KubeRBACProbe struct {
	// KubeconfigPath is the path to the scoped kubeconfig for this check.
	KubeconfigPath string

	// Namespace is the Kubernetes namespace to check permissions in.
	Namespace string

	// Group is the API group (e.g. "hedera.com"). Use "" for core resources.
	Group string

	// Resource is the plural resource name (e.g. "networkupgradeexecutes").
	Resource string

	// Verbs are the access verbs to verify (e.g. ["list", "watch"]).
	Verbs []string
}

// Probe implements daemon.Probe. Retries every kubeRBACRetryInterval until all
// verbs are allowed or ctx is cancelled. Returns ctx.Err() on cancellation.
func (p *KubeRBACProbe) Probe(ctx context.Context) error {
	attempt := 0
	for {
		attempt++
		if err := p.once(ctx); err == nil {
			logx.As().Info().
				Str("reason", "KubeRBACProbeSuccess").
				Str("resource", fmt.Sprintf("%s/%s", p.Group, p.Resource)).
				Str("namespace", p.Namespace).
				Int("attempt", attempt).
				Msg("Kubernetes RBAC probe succeeded")
			return nil
		} else {
			logx.As().Warn().Err(err).
				Str("reason", "KubeRBACProbeFailed").
				Str("resource", fmt.Sprintf("%s/%s", p.Group, p.Resource)).
				Str("namespace", p.Namespace).
				Int("attempt", attempt).
				Msg("Kubernetes RBAC probe failed — retrying")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(kubeRBACRetryInterval):
		}
	}
}

// once performs a single SelfSubjectAccessReview for each configured verb.
// Returns nil only when all verbs are allowed.
func (p *KubeRBACProbe) once(ctx context.Context) error {
	restCfg, err := clientcmd.BuildConfigFromFlags("", p.KubeconfigPath)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "build kubeconfig")
	}
	restCfg.Timeout = kubeRBACRESTTimeout
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "build kube client")
	}
	for _, verb := range p.Verbs {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: p.Namespace,
					Verb:      verb,
					Group:     p.Group,
					Resource:  p.Resource,
				},
			},
		}
		result, err := client.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, review, metav1.CreateOptions{})
		if err != nil {
			return errorx.ExternalError.Wrap(err, "SelfSubjectAccessReview(%s)", verb)
		}
		if !result.Status.Allowed {
			return errorx.IllegalState.New("RBAC denied: verb=%s resource=%s.%s namespace=%s",
				verb, p.Resource, p.Group, p.Namespace)
		}
	}
	return nil
}
