package steps

import (
	"context"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/kube"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

const (
	defaultClusterResourceCheckTimeout = 60 * time.Second
	CheckClusterNodesStepId            = "check_cluster_nodes"
	CheckClusterNamespacesStepId       = "check_cluster_namespaces"
	CheckClusterConfigMapsStepId       = "check_cluster_configmaps"
	CheckClusterPodsStepId             = "check_cluster_pods"
	CheckClusterServicesStepId         = "check_cluster_services"
	CheckClusterCRDsStepId             = "check_cluster_crds"
)

func splitIntoNamespaceAndName(item string) (string, string) {
	parts := strings.SplitN(item, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// list of critical cluster pods to check for readiness
// Format: namespace/pod-name-prefix
var clusterPods = []string{
	"kube-system/cilium-",
	"kube-system/cilium-operator-",
	"kube-system/coredns-",
	"kube-system/etcd-",
	"kube-system/hubble-relay-",
	"kube-system/kube-apiserver-",
	"kube-system/kube-controller-manager-",
	"kube-system/kube-scheduler-",
	"metallb-system/metallb-controller-",
	"metallb-system/metallb-speaker-",
}

// list of critical cluster services to check for presence
// Format: namespace/service-name
var clusterServices = []string{
	"default/kubernetes",
	"kube-system/hubble-peer",
	"kube-system/hubble-relay",
	"kube-system/kube-dns",
	"metallb-system/metallb-webhook-service",
}

var clusterNamespaces = []string{
	"default",
	"kube-node-lease",
	"kube-public",
	"kube-system",
	"cilium-secrets",
	"metallb-system",
}

// list of critical cluster config maps to check for presence
// Format: namespace/config-map-name
var clusterConfigMaps = []string{
	"cilium-secrets/kube-root-ca.crt",
	"default/kube-root-ca.crt",
	"kube-node-lease/kube-root-ca.crt",
	"kube-public/cluster-info",
	"kube-public/kube-root-ca.crt",
	"kube-system/cilium-config",
	"kube-system/coredns",
	"kube-system/extension-apiserver-authentication",
	"kube-system/hubble-relay-config",
	"kube-system/ip-masq-agent",
	"kube-system/kube-apiserver-legacy-service-account-token-tracking",
	"kube-system/kube-root-ca.crt",
	"kube-system/kubeadm-config",
	"kube-system/kubelet-config",
	"metallb-system/kube-root-ca.crt",
	"metallb-system/metallb-excludel2",
}

var clusterCRDs = []string{
	"bfdprofiles.metallb.io",
	"bgpadvertisements.metallb.io",
	"bgppeers.metallb.io",
	"ciliumcidrgroups.cilium.io",
	"ciliumclusterwidenetworkpolicies.cilium.io",
	"ciliumendpoints.cilium.io",
	"ciliumidentities.cilium.io",
	"ciliuml2announcementpolicies.cilium.io",
	"ciliumloadbalancerippools.cilium.io",
	"ciliumnetworkpolicies.cilium.io",
	"ciliumnodeconfigs.cilium.io",
	"ciliumnodes.cilium.io",
	"ciliumpodippools.cilium.io",
	"communities.metallb.io",
	"ipaddresspools.metallb.io",
	"l2advertisements.metallb.io",
	"servicebgpstatuses.metallb.io",
	"servicel2statuses.metallb.io",
}

// This helps to avoid instantiating multiple kube clients during the health check workflow
var kubeClient *kube.Client
var kubeClientProvider = func() (*kube.Client, error) {
	if kubeClient != nil {
		return kubeClient, nil
	}

	k, err := kube.NewClient()
	if err != nil {
		return nil, err
	}

	kubeClient = k
	return kubeClient, nil
}

// CheckClusterHealth performs a series of checks to ensure the cluster is healthy and operational
func CheckClusterHealth() automa.Builder {
	kubeClient = nil // reset kube client before starting the health check workflow

	return automa.NewWorkflowBuilder().WithId("check-cluster-health").Steps(
		CheckClusterNodesReady(CheckClusterNodesStepId, kubeClientProvider),
		CheckClusterNamespaces(CheckClusterNamespacesStepId, clusterNamespaces, defaultClusterResourceCheckTimeout, kubeClientProvider),
		CheckClusterConfigMaps(CheckClusterConfigMapsStepId, clusterConfigMaps, defaultClusterResourceCheckTimeout, kubeClientProvider),
		CheckClusterCRDs(CheckClusterCRDsStepId, clusterCRDs, defaultClusterResourceCheckTimeout, kubeClientProvider),
		CheckClusterPodsReady(CheckClusterPodsStepId, clusterPods, defaultClusterResourceCheckTimeout, kubeClientProvider),
		CheckClusterServices(CheckClusterServicesStepId, clusterServices, defaultClusterResourceCheckTimeout, kubeClientProvider),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster health")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Cluster health check failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster is healthy")
		})
}
