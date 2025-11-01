#!/bin/bash
set -e

# Check if kubectl can access the cluster
kubectl get nodes

# Check all nodes are Ready
kubectl get nodes --no-headers | awk '{print $2}' | grep -qv '^Ready$'
if [ $? -eq 0 ]; then exit 1; fi

# List of required namespaces
namespaces="cilium-secrets default kube-node-lease kube-public kube-system metallb-system"

for ns in $namespaces; do
  status=$(kubectl get namespace $ns --no-headers 2>/dev/null | awk '{print $2}')
  if [ "$status" != "Active" ]; then
    echo "Namespace $ns is not Active or does not exist (status: $status)"
    exit 1
  fi
done

# List of pod name prefixes to check in kube-system
prefixes="cilium- cilium-operator- coredns- etcd- hubble-relay- kube-apiserver- kube-controller-manager- kube-scheduler- metallb-controller- metallb-speaker-"

for prefix in $prefixes; do
  kubectl get pods -n kube-system --no-headers | awk -v p="$prefix" '$1 ~ "^"p {print $1, $2}' | while read name ready; do
    if ! [[ "$ready" =~ ^([0-9]+)/\1$ ]]; then
      echo "Pod $name is not fully ready ($ready)"
      exit 1
    fi
  done
done

# List of required services in the format namespace:service
services="default:kubernetes kube-system:hubble-peer kube-system:hubble-relay kube-system:kube-dns metallb-system:metallb-webhook-service"

for svc in $services; do
  ns=$(echo $svc | cut -d: -f1)
  name=$(echo $svc | cut -d: -f2)
  if ! kubectl get svc -n "$ns" "$name" --no-headers 2>/dev/null | grep -q .; then
    echo "Service $name not found in namespace $ns"
    exit 1
  fi
done

# List of required CRDs
crds="bfdprofiles.metallb.io bgpadvertisements.metallb.io bgppeers.metallb.io ciliumcidrgroups.cilium.io ciliumclusterwidenetworkpolicies.cilium.io ciliumendpoints.cilium.io ciliumidentities.cilium.io ciliuml2announcementpolicies.cilium.io ciliumloadbalancerippools.cilium.io ciliumnetworkpolicies.cilium.io ciliumnodeconfigs.cilium.io ciliumnodes.cilium.io ciliumpodippools.cilium.io communities.metallb.io ipaddresspools.metallb.io l2advertisements.metallb.io servicebgpstatuses.metallb.io servicel2statuses.metallb.io"

for crd in $crds; do
  if ! kubectl get crd "$crd" --no-headers 2>/dev/null | grep -q .; then
    echo "CRD $crd not found"
    exit 1
  fi
done

echo "Cluster health check passed"
