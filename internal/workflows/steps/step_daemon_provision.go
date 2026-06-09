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
	"github.com/hashgraph/solo-weaver/pkg/models"
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
	daemonSAName          = "solo-provisioner-daemon"
	daemonClusterRoleName = "solo-provisioner-daemon"
	daemonCRBName         = "solo-provisioner-daemon"
	daemonTokenSecretName = "solo-provisioner-daemon-token"

	daemonRBACGroup    = "hedera.com"
	daemonRBACResource = "networkupgradeexecutes"

	// tokenReadyTimeout is how long we wait for the K8s token controller to
	// populate the SA token secret after creation.
	tokenReadyTimeout  = 30 * time.Second
	tokenReadyInterval = 500 * time.Millisecond
)

// daemonRBACCreated tracks which RBAC resources were actually created by
// CreateDaemonRBACStep on this run so the rollback only removes what it made.
type daemonRBACCreated struct {
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
						"K8s cluster is not reachable — ensure ~/.kube/config is valid and the API server is up")))
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

// CreateDaemonRBACStep idempotently creates the ServiceAccount, ClusterRole,
// ClusterRoleBinding, and long-lived token Secret needed by the daemon. If any
// resource already exists it is left unchanged. Rollback only removes resources
// that were actually created on this run — pre-existing resources are left in
// place so a failed re-install does not invalidate a working prior installation.
func CreateDaemonRBACStep(namespace string) *automa.StepBuilder {
	// created is captured by both Execute and Rollback closures so the rollback
	// knows exactly which resources to undo.
	var created daemonRBACCreated

	return automa.NewStepBuilder().WithId("create-daemon-rbac").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cs, err := newTypedClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// 1. ServiceAccount
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: daemonSAName, Namespace: namespace},
			}
			if _, err := cs.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, metav1.CreateOptions{}); err != nil {
				if !kerrors.IsAlreadyExists(err) {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to create ServiceAccount %s", daemonSAName)))
				}
				logx.As().Debug().Str("sa", daemonSAName).Msg("ServiceAccount already exists — skipping")
			} else {
				created.sa = true
			}

			// 2. ClusterRole
			cr := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: daemonClusterRoleName},
				Rules: []rbacv1.PolicyRule{{
					APIGroups: []string{daemonRBACGroup},
					Resources: []string{daemonRBACResource},
					Verbs:     []string{"list", "watch"},
				}},
			}
			if _, err := cs.RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{}); err != nil {
				if !kerrors.IsAlreadyExists(err) {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to create ClusterRole %s", daemonClusterRoleName)))
				}
				logx.As().Debug().Str("cr", daemonClusterRoleName).Msg("ClusterRole already exists — skipping")
			} else {
				created.cr = true
			}

			// 3. ClusterRoleBinding
			crb := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: daemonCRBName},
				Subjects: []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      daemonSAName,
					Namespace: namespace,
				}},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     daemonClusterRoleName,
				},
			}
			if _, err := cs.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil {
				if !kerrors.IsAlreadyExists(err) {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to create ClusterRoleBinding %s", daemonCRBName)))
				}
				logx.As().Debug().Str("crb", daemonCRBName).Msg("ClusterRoleBinding already exists — skipping")
			} else {
				created.crb = true
			}

			// 4. Long-lived token Secret — annotated with the SA name so the
			// token controller populates it automatically.
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      daemonTokenSecretName,
					Namespace: namespace,
					Annotations: map[string]string{
						corev1.ServiceAccountNameKey: daemonSAName,
					},
				},
				Type: corev1.SecretTypeServiceAccountToken,
			}
			if _, err := cs.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
				if !kerrors.IsAlreadyExists(err) {
					return automa.StepFailureReport(stp.Id(),
						automa.WithError(errorx.InternalError.Wrap(err, "failed to create token Secret %s", daemonTokenSecretName)))
				}
				logx.As().Debug().Str("secret", daemonTokenSecretName).Msg("Token Secret already exists — skipping")
			} else {
				created.secret = true
			}

			logx.As().Info().
				Str("namespace", namespace).
				Str("sa", daemonSAName).
				Str("cr", daemonClusterRoleName).
				Str("crb", daemonCRBName).
				Str("secret", daemonTokenSecretName).
				Msg("Daemon RBAC resources created")
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
			deleteCreatedDaemonRBAC(ctx, namespace, created)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create daemon RBAC resources")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon RBAC resources created")
		})
}

// WriteDaemonKubeconfigStep waits for the SA token Secret to be populated then
// writes a kubeconfig file at paths.DaemonKubeconfigPath using the SA token and
// cluster CA from the admin kubeconfig. The file is written with mode 0600 (root
// only) since it contains a service account credential. Rollback removes the file.
func WriteDaemonKubeconfigStep(paths models.WeaverPaths, namespace string) *automa.StepBuilder {
	kubeconfigPath := paths.DaemonKubeconfigPath
	return automa.NewStepBuilder().WithId("write-daemon-kubeconfig").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cs, err := newTypedClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			token, ca, server, err := waitForSAToken(ctx, cs, namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "timed out waiting for SA token secret to be populated")))
			}

			if err := writeDaemonKubeconfig(kubeconfigPath, server, ca, token); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err, "failed to write daemon kubeconfig to %s", kubeconfigPath)))
			}

			logx.As().Info().Str("path", kubeconfigPath).Msg("Daemon kubeconfig written")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Writing daemon kubeconfig")
			return ctx, nil
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			_ = os.Remove(kubeconfigPath)
			return automa.StepSuccessReport(stp.Id())
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to write daemon kubeconfig")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon kubeconfig written")
		})
}

// RemoveDaemonKubeconfigStep removes the daemon kubeconfig file. Removal is
// best-effort: a missing file is noted at Info level, a real removal error is
// logged as a warning and the step still succeeds so uninstall can continue.
func RemoveDaemonKubeconfigStep(paths models.WeaverPaths) *automa.StepBuilder {
	kubeconfigPath := paths.DaemonKubeconfigPath
	return automa.NewStepBuilder().WithId("remove-daemon-kubeconfig").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := os.Remove(kubeconfigPath); err != nil {
				if os.IsNotExist(err) {
					logx.As().Info().Str("path", kubeconfigPath).Msg("Daemon kubeconfig already absent")
				} else {
					logx.As().Warn().Err(err).Str("path", kubeconfigPath).Msg("Failed to remove daemon kubeconfig")
				}
			} else {
				logx.As().Info().Str("path", kubeconfigPath).Msg("Daemon kubeconfig removed")
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing daemon kubeconfig")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove daemon kubeconfig")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Daemon kubeconfig removed")
		})
}

// DeleteDaemonRBACStep deletes the ClusterRoleBinding, ClusterRole, token
// Secret, and ServiceAccount. All deletions are best-effort — missing resources
// are silently ignored, other errors are logged as warnings. The step always
// succeeds; if individual deletes warned, the summary log says so explicitly.
func DeleteDaemonRBACStep(namespace string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("delete-daemon-rbac").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			allClean := deleteDaemonRBAC(ctx, namespace)
			if allClean {
				logx.As().Info().Str("namespace", namespace).Msg("Daemon RBAC resources deleted")
			} else {
				logx.As().Warn().Str("namespace", namespace).
					Msg("Daemon RBAC resources deletion completed with warnings — some resources may not have been removed (see above)")
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

// deleteCreatedDaemonRBAC deletes only the resources flagged in created. Used
// by the rollback path so pre-existing resources are not disturbed.
// Returns true if all attempted deletes succeeded (or the resource was already
// gone); false if any delete logged a warning.
func deleteCreatedDaemonRBAC(ctx context.Context, namespace string, created daemonRBACCreated) bool {
	if !created.sa && !created.cr && !created.crb && !created.secret {
		return true
	}
	cs, err := newTypedClient()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to build kube client for RBAC rollback")
		return false
	}
	allClean := true
	del := metav1.DeleteOptions{}
	if created.crb {
		if err := cs.RbacV1().ClusterRoleBindings().Delete(ctx, daemonCRBName, del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("crb", daemonCRBName).Msg("Rollback: failed to delete ClusterRoleBinding")
			allClean = false
		}
	}
	if created.cr {
		if err := cs.RbacV1().ClusterRoles().Delete(ctx, daemonClusterRoleName, del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("cr", daemonClusterRoleName).Msg("Rollback: failed to delete ClusterRole")
			allClean = false
		}
	}
	if created.secret {
		if err := cs.CoreV1().Secrets(namespace).Delete(ctx, daemonTokenSecretName, del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("secret", daemonTokenSecretName).Msg("Rollback: failed to delete token Secret")
			allClean = false
		}
	}
	if created.sa {
		if err := cs.CoreV1().ServiceAccounts(namespace).Delete(ctx, daemonSAName, del); err != nil && !kerrors.IsNotFound(err) {
			logx.As().Warn().Err(err).Str("sa", daemonSAName).Msg("Rollback: failed to delete ServiceAccount")
			allClean = false
		}
	}
	return allClean
}

// deleteDaemonRBAC deletes all four RBAC resources unconditionally. Used by the
// uninstall step. Errors are logged as warnings and do not abort the caller.
// Returns true if all deletes were clean.
func deleteDaemonRBAC(ctx context.Context, namespace string) bool {
	return deleteCreatedDaemonRBAC(ctx, namespace, daemonRBACCreated{sa: true, cr: true, crb: true, secret: true})
}

// waitForSAToken polls until the token controller has populated the SA token
// Secret, then returns the token, CA data, and API server URL extracted from
// the admin kubeconfig.
func waitForSAToken(ctx context.Context, cs *kubernetes.Clientset, namespace string) (token, ca, server string, err error) {
	deadline := time.Now().Add(tokenReadyTimeout)
	for {
		secret, getErr := cs.CoreV1().Secrets(namespace).Get(ctx, daemonTokenSecretName, metav1.GetOptions{})
		if getErr == nil && len(secret.Data["token"]) > 0 {
			token = string(secret.Data["token"])
			ca = string(secret.Data["ca.crt"])
			break
		}

		if time.Now().After(deadline) {
			return "", "", "", errorx.InternalError.New(
				"SA token secret %s/%s not populated within %s", namespace, daemonTokenSecretName, tokenReadyTimeout)
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

// writeDaemonKubeconfig writes a minimal kubeconfig for the daemon SA to path.
// The file is created with 0600 permissions (root-readable only).
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
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return errorx.InternalError.Wrap(err, "failed to write kubeconfig to %s", path)
	}
	return nil
}
