// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	// tokenReadyTimeout is how long we wait for the K8s token controller to
	// populate the SA token secret after creation.
	tokenReadyTimeout  = 30 * time.Second
	tokenReadyInterval = 500 * time.Millisecond
)

// DaemonComponentSpec describes the K8s resources the daemon install workflow
// must create for one K8s-dependent daemon component. The workflow builds one
// spec per enabled component from the DaemonConfig and passes the slice to the
// generic RBAC and kubeconfig steps — adding a new component (e.g. block-node)
// therefore requires only a new spec entry, no step code changes.
type DaemonComponentSpec struct {
	// ShortName is the suffix appended to all K8s resource names created for
	// this component (e.g. "cn" → SA solo-provisioner-daemon-cn, ClusterRole
	// solo-provisioner-daemon-cn, token secret solo-provisioner-daemon-cn-token).
	ShortName string

	// Namespace is the orbit namespace where the SA and token Secret live.
	Namespace string

	// KubeconfigPath is the absolute path where the component's scoped
	// kubeconfig is written (e.g. /opt/solo/weaver/config/daemon-cn.kubeconfig).
	KubeconfigPath string

	// PolicyRules are the RBAC permissions granted to the component's
	// ClusterRole. Each component declares only the rules it needs so that
	// the principle of least privilege is maintained per component.
	PolicyRules []rbacv1.PolicyRule
}

func (s DaemonComponentSpec) saName() string { return "solo-provisioner-daemon-" + s.ShortName }
func (s DaemonComponentSpec) clusterRoleName() string {
	return "solo-provisioner-daemon-" + s.ShortName
}
func (s DaemonComponentSpec) crbName() string { return "solo-provisioner-daemon-" + s.ShortName }
func (s DaemonComponentSpec) tokenSecretName() string {
	return "solo-provisioner-daemon-" + s.ShortName + "-token"
}

// componentRBACCreated tracks which K8s resources were actually created for one
// component during CreateDaemonRBACStep so that rollback only removes what was
// made on this run — pre-existing resources are left intact.
type componentRBACCreated struct {
	sa     bool
	cr     bool
	crb    bool
	secret bool
}

// newTypedClient builds a typed kubernetes.Clientset from ~/.kube/config (or
// KUBECONFIG env var), matching the same kubeconfig resolution as kube.NewClient.
func newTypedClient() (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to load kubeconfig")
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "failed to create kubernetes client")
	}
	return cs, nil
}

// CheckClusterStep verifies the K8s API is reachable via the admin kubeconfig
// before attempting any provisioning.
func CheckClusterStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("check-cluster").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			exists, err := kube.ClusterExists()
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to check cluster reachability")))
			}
			if !exists {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.IllegalState.New(
						"K8s cluster is not reachable — ensure ~/.kube/config is valid and the API server is up").
						WithProperty(models.ErrPropertyResolution, []string{
							"Verify kubeconfig: kubectl cluster-info",
							"Check KUBECONFIG env var or ~/.kube/config is present and points to a running cluster",
							"Ensure the K8s API server is reachable from this host",
						})))
			}
			logx.As().Info().Msg("K8s cluster is reachable")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking K8s cluster reachability")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "K8s cluster check failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "K8s cluster is reachable")
		})
}

// CreateDaemonRBACStep idempotently creates, for each component in specs, one
// ServiceAccount, ClusterRole, ClusterRoleBinding, and long-lived token Secret.
// Resources that already exist are left unchanged. The rollback only removes
// resources that were actually created on this run so a failed re-install does
// not invalidate a prior working installation.
//
// Resource names follow the convention solo-provisioner-daemon-<shortName> so
// that components are isolated and independently upgradeable.
func CreateDaemonRBACStep(specs []DaemonComponentSpec) *automa.StepBuilder {
	// created is captured by both Execute and Rollback closures.
	// created[i] corresponds to specs[i].
	created := make([]componentRBACCreated, len(specs))

	return automa.NewStepBuilder().WithId("create-daemon-rbac").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cs, err := newTypedClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for i, spec := range specs {
				// 1. ServiceAccount
				sa := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: spec.saName(), Namespace: spec.Namespace},
				}
				if _, err := cs.CoreV1().ServiceAccounts(spec.Namespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil {
					if !kerrors.IsAlreadyExists(err) {
						orbitFlag := "--cn-orbit"
						if spec.ShortName == "bn" {
							orbitFlag = "--bn-orbit"
						}
						return automa.StepFailureReport(stp.Id(),
							automa.WithError(errorx.InternalError.Wrap(err, "failed to create ServiceAccount %s", spec.saName()).
								WithProperty(models.ErrPropertyResolution, []string{
									"Ensure the orbit namespace exists: kubectl get ns " + spec.Namespace,
									"Create the namespace if missing: kubectl create ns " + spec.Namespace,
									"Verify kubeconfig permission: kubectl auth can-i create serviceaccounts -n " + spec.Namespace,
									"Re-run after the namespace is ready: sudo solo-provisioner daemon service install " + orbitFlag + "=" + spec.Namespace,
								})))
					}
					logx.As().Debug().Str("sa", spec.saName()).Msg("ServiceAccount already exists — skipping")
				} else {
					created[i].sa = true
				}

				// 2. ClusterRole
				cr := &rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{Name: spec.clusterRoleName()},
					Rules:      spec.PolicyRules,
				}
				if _, err := cs.RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{}); err != nil {
					if !kerrors.IsAlreadyExists(err) {
						return automa.StepFailureReport(stp.Id(),
							automa.WithError(errorx.InternalError.Wrap(err, "failed to create ClusterRole %s", spec.clusterRoleName())))
					}
					logx.As().Debug().Str("cr", spec.clusterRoleName()).Msg("ClusterRole already exists — skipping")
				} else {
					created[i].cr = true
				}

				// 3. ClusterRoleBinding
				crb := &rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: spec.crbName()},
					Subjects: []rbacv1.Subject{{
						Kind:      "ServiceAccount",
						Name:      spec.saName(),
						Namespace: spec.Namespace,
					}},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     spec.clusterRoleName(),
					},
				}
				if _, err := cs.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil {
					if !kerrors.IsAlreadyExists(err) {
						return automa.StepFailureReport(stp.Id(),
							automa.WithError(errorx.InternalError.Wrap(err, "failed to create ClusterRoleBinding %s", spec.crbName())))
					}
					logx.As().Debug().Str("crb", spec.crbName()).Msg("ClusterRoleBinding already exists — skipping")
				} else {
					created[i].crb = true
				}

				// 4. Long-lived token Secret — annotated with the SA name so the
				// token controller populates it automatically.
				//
				// DESIGN: deliberately a non-expiring SecretTypeServiceAccountToken
				// rather than a bounded TokenRequest token. The daemon is a
				// long-running, unattended process on a node where the design goal
				// is zero human intervention. A bounded token would require the
				// daemon to refresh it before expiry; any gap in that refresh path
				// (daemon down during the window, API unreachable, clock skew) would
				// silently break automation and demand exactly the manual recovery
				// we are trying to eliminate. A standing credential trades a larger
				// blast radius for unattended reliability.
				//
				// The blast radius is bounded by the ClusterRole granted to this SA
				// (see step 2 above) — the credential is only as powerful as that
				// RBAC, and it lives in the orbit namespace readable by the daemon.
				// The token is revoked by deleting this Secret + SA (DeleteDaemonRBACStep),
				// which is how rotation is performed: re-provision to mint a fresh one.
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      spec.tokenSecretName(),
						Namespace: spec.Namespace,
						Annotations: map[string]string{
							corev1.ServiceAccountNameKey: spec.saName(),
						},
					},
					Type: corev1.SecretTypeServiceAccountToken,
				}
				if _, err := cs.CoreV1().Secrets(spec.Namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
					if !kerrors.IsAlreadyExists(err) {
						return automa.StepFailureReport(stp.Id(),
							automa.WithError(errorx.InternalError.Wrap(err, "failed to create token Secret %s", spec.tokenSecretName())))
					}
					logx.As().Debug().Str("secret", spec.tokenSecretName()).Msg("Token Secret already exists — skipping")
				} else {
					created[i].secret = true
				}

				logx.As().Info().
					Str("component", spec.ShortName).
					Str("namespace", spec.Namespace).
					Str("sa", spec.saName()).
					Str("cr", spec.clusterRoleName()).
					Str("crb", spec.crbName()).
					Str("secret", spec.tokenSecretName()).
					Msg("Daemon RBAC resources created")
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating daemon RBAC resources")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Only delete what this run actually created — leave pre-existing
			// resources in place so a failed re-install does not break a prior
			// working installation.
			for i, spec := range specs {
				deleteCreatedComponentRBAC(ctx, spec, created[i])
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create daemon RBAC resources")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon RBAC resources created")
		})
}

// WriteDaemonKubeconfigStep waits for each component's SA token Secret to be
// populated, then writes a scoped kubeconfig to spec.KubeconfigPath using the
// SA token and cluster CA from the admin kubeconfig. Files are written with mode
// 0640 (root:weaver) so the daemon process (running as the weaver group) can
// read them. Rollback removes all kubeconfig files written by this step.
func WriteDaemonKubeconfigStep(specs []DaemonComponentSpec) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("write-daemon-kubeconfigs").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cs, err := newTypedClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, spec := range specs {
				token, ca, server, err := waitForSAToken(ctx, cs, spec.Namespace, spec.tokenSecretName())
				if err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err,
							"timed out waiting for SA token secret %s to be populated", spec.tokenSecretName())))
				}
				if err := writeDaemonKubeconfig(spec.KubeconfigPath, server, ca, token); err != nil {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err,
							"failed to write daemon kubeconfig for component %s to %s", spec.ShortName, spec.KubeconfigPath)))
				}
				logx.As().Info().
					Str("component", spec.ShortName).
					Str("path", spec.KubeconfigPath).
					Msg("Daemon kubeconfig written")
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Writing daemon kubeconfigs")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			for _, spec := range specs {
				_ = os.Remove(spec.KubeconfigPath)
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to write daemon kubeconfigs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon kubeconfigs written")
		})
}

// RemoveDaemonKubeconfigStep removes the kubeconfig file for every component in
// specs. Removal is best-effort: a missing file is noted at Info level, a real
// removal error is logged as a warning and the step still succeeds so uninstall
// can continue past partial state.
func RemoveDaemonKubeconfigStep(specs []DaemonComponentSpec) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("remove-daemon-kubeconfigs").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			for _, spec := range specs {
				if err := os.Remove(spec.KubeconfigPath); err != nil {
					if os.IsNotExist(err) {
						logx.As().Info().
							Str("component", spec.ShortName).
							Str("path", spec.KubeconfigPath).
							Msg("Daemon kubeconfig already absent")
					} else {
						logx.As().Warn().Err(err).
							Str("component", spec.ShortName).
							Str("path", spec.KubeconfigPath).
							Msg("Failed to remove daemon kubeconfig")
					}
				} else {
					logx.As().Info().
						Str("component", spec.ShortName).
						Str("path", spec.KubeconfigPath).
						Msg("Daemon kubeconfig removed")
				}
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing daemon kubeconfigs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove daemon kubeconfigs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon kubeconfigs removed")
		})
}

// DeleteDaemonRBACStep deletes the ClusterRoleBinding, ClusterRole, token Secret,
// and ServiceAccount for every component in specs. All deletions are best-effort —
// missing resources are silently ignored, other errors are logged as warnings. The
// step always succeeds so uninstall can continue past partial state.
func DeleteDaemonRBACStep(specs []DaemonComponentSpec) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("delete-daemon-rbac").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			for _, spec := range specs {
				allClean := deleteAllComponentRBAC(ctx, spec)
				if allClean {
					logx.As().Info().
						Str("component", spec.ShortName).
						Str("namespace", spec.Namespace).
						Msg("Daemon RBAC resources deleted")
				} else {
					logx.As().Warn().
						Str("component", spec.ShortName).
						Str("namespace", spec.Namespace).
						Msg("Daemon RBAC deletion completed with warnings — some resources may not have been removed (see above)")
				}
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deleting daemon RBAC resources")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete daemon RBAC resources")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon RBAC resources deleted")
		})
}

// deleteCreatedComponentRBAC deletes only the resources flagged in created for
// the given spec. Used by the rollback path so pre-existing resources are not
// disturbed. Returns true if all attempted deletes succeeded.
func deleteCreatedComponentRBAC(ctx context.Context, spec DaemonComponentSpec, created componentRBACCreated) bool {
	if !created.sa && !created.cr && !created.crb && !created.secret {
		return true
	}
	cs, err := newTypedClient()
	if err != nil {
		logx.As().Warn().Err(err).Str("component", spec.ShortName).Msg("failed to build kube client for RBAC cleanup")
		return false
	}
	allClean := true
	del := metav1.DeleteOptions{}
	if created.crb {
		if err := cs.RbacV1().ClusterRoleBindings().Delete(ctx, spec.crbName(), del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("crb", spec.crbName()).Msg("failed to delete ClusterRoleBinding")
			allClean = false
		}
	}
	if created.cr {
		if err := cs.RbacV1().ClusterRoles().Delete(ctx, spec.clusterRoleName(), del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("cr", spec.clusterRoleName()).Msg("failed to delete ClusterRole")
			allClean = false
		}
	}
	// Delete SA before Secret: K8s garbage-collects the owned token Secret when the SA
	// is removed, so deleting SA first avoids a race where the token controller adds a
	// finalizer to the SA while the Secret deletion is in flight.
	if created.sa {
		if err := cs.CoreV1().ServiceAccounts(spec.Namespace).Delete(ctx, spec.saName(), del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("sa", spec.saName()).Msg("failed to delete ServiceAccount")
			allClean = false
		}
	}
	if created.secret {
		if err := cs.CoreV1().Secrets(spec.Namespace).Delete(ctx, spec.tokenSecretName(), del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("secret", spec.tokenSecretName()).Msg("failed to delete token Secret")
			allClean = false
		}
	}
	return allClean
}

// deleteAllComponentRBAC deletes all four RBAC resources for spec unconditionally.
// Used by the uninstall step. Returns true if all deletes were clean.
func deleteAllComponentRBAC(ctx context.Context, spec DaemonComponentSpec) bool {
	return deleteCreatedComponentRBAC(ctx, spec, componentRBACCreated{sa: true, cr: true, crb: true, secret: true})
}

// waitForSAToken polls until the token controller has populated the SA token
// Secret identified by secretName in namespace, then returns the token, CA data,
// and API server URL extracted from the admin kubeconfig.
func waitForSAToken(ctx context.Context, cs *kubernetes.Clientset, namespace, secretName string) (token, ca, server string, err error) {
	deadline := time.Now().Add(tokenReadyTimeout)
	for {
		secret, getErr := cs.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
		if getErr == nil && len(secret.Data["token"]) > 0 {
			token = string(secret.Data["token"])
			ca = string(secret.Data["ca.crt"])
			break
		}

		if time.Now().After(deadline) {
			return "", "", "", errorx.InternalError.New(
				"SA token secret %s/%s not populated within %s", namespace, secretName, tokenReadyTimeout)
		}

		select {
		case <-ctx.Done():
			return "", "", "", errorx.InternalError.Wrap(ctx.Err(), "context cancelled waiting for SA token")
		case <-time.After(tokenReadyInterval):
		}
	}

	// Extract the API server URL from the active context in the admin kubeconfig.
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	rawCfg, cfgErr := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{},
	).RawConfig()
	if cfgErr != nil {
		return "", "", "", errorx.InternalError.Wrap(cfgErr, "failed to read admin kubeconfig")
	}
	currentCtx := rawCfg.CurrentContext
	if currentCtx == "" {
		return "", "", "", errorx.IllegalState.New("admin kubeconfig has no current-context set")
	}
	ktx, ok := rawCfg.Contexts[currentCtx]
	if !ok {
		return "", "", "", errorx.IllegalState.New("context %q not found in admin kubeconfig", currentCtx)
	}
	cluster, ok := rawCfg.Clusters[ktx.Cluster]
	if !ok {
		return "", "", "", errorx.IllegalState.New("cluster %q not found in admin kubeconfig", ktx.Cluster)
	}
	server = cluster.Server
	return token, ca, server, nil
}

// AddOperatorToWeaverGroupStep adds the invoking operator (SUDO_USER) to the
// weaver group so they can reach the daemon socket without sudo. The step is
// idempotent — if the user is already a member it logs and succeeds. If
// SUDO_USER is unset or is "root" the step is skipped silently.
func AddOperatorToWeaverGroupStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("add-operator-to-weaver-group").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			sudoUser := os.Getenv("SUDO_USER")
			if sudoUser == "" || sudoUser == "root" {
				logx.As().Debug().Str("reason", "AddOperatorToWeaverGroupSkipped").
					Msg("SUDO_USER not set or is root — skipping weaver group membership")
				return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{"skipped": "true"}))
			}
			// Validate before passing to usermod — env vars can be manipulated.
			if err := sanity.ValidateUsername(sudoUser); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.IllegalState.Wrap(err, "invalid SUDO_USER environment variable: %s", sudoUser)))
			}

			pm, err := principal.NewManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			weaverGroup := config.WeaverGroupName()
			if err := pm.AddUserToGroup(sudoUser, weaverGroup); err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to add %s to group %s — fix with: sudo usermod -aG %s %s",
						sudoUser, weaverGroup, weaverGroup, sudoUser)))
			}

			logx.As().Info().Str("user", sudoUser).Str("group", weaverGroup).
				Str("reason", "OperatorAddedToWeaverGroup").
				Msgf("Added %s to group %s — re-login or run `newgrp %s` to activate", sudoUser, weaverGroup, weaverGroup)
			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{"user": sudoUser, "group": weaverGroup}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Adding operator to weaver group")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			return automa.SuccessReport(stp)
		})
}

// writeDaemonKubeconfig writes a minimal kubeconfig for the daemon SA to path.
// The file is created with 0640 (root:weaver) so the daemon can read it.
func writeDaemonKubeconfig(path, server, ca, token string) error {
	cfg := clientcmdapi.NewConfig()
	cfg.Clusters["solo-weaver"] = &clientcmdapi.Cluster{
		Server:                   server,
		CertificateAuthorityData: []byte(ca),
	}
	cfg.AuthInfos["solo-provisioner-daemon"] = &clientcmdapi.AuthInfo{
		Token: token,
	}
	cfg.Contexts["solo-weaver"] = &clientcmdapi.Context{
		Cluster:  "solo-weaver",
		AuthInfo: "solo-provisioner-daemon",
	}
	cfg.CurrentContext = "solo-weaver"

	data, err := clientcmd.Write(*cfg)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to serialize kubeconfig")
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return errorx.InternalError.Wrap(err, "failed to write kubeconfig to %s", path)
	}
	return nil
}
