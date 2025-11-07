#!/bin/bash
# Updated health check with icons and robust parsing
# File: 'test/scripts/health.sh'

green="\033[0;32m"
red="\033[0;31m"
yellow="\033[0;33m"
nc="\033[0m"

success() {
  printf "%b ✅ %s%b\n" "$green" "$1" "$nc"
}

fail() {
  printf "%b ❌ %s%b\n" "$red" "$1" "$nc"
  exit 1
}

warn() {
  printf "%b ⚠️ %s%b\n" "$yellow" "$1" "$nc"
}

# Check if kubectl can access the cluster
printf ">> Checking cluster access...\n"
if sudo kubectl get nodes >/dev/null 2>&1; then
  success "Cluster access verified."
else
  fail "Cannot access the cluster with kubectl."
fi

# Check all nodes are Ready
printf ">> Checking node statuses...\n"
not_ready=$(sudo kubectl get nodes --no-headers 2>/dev/null | awk '{print $2}' | grep -v '^Ready$' || true)
if [ -n "$not_ready" ]; then
  fail "Some nodes are not Ready: $not_ready"
else
  success "All nodes are Ready."
fi

# List of required namespaces
printf ">> Checking required namespaces...\n"
namespaces="cilium-secrets default kube-node-lease kube-public kube-system metallb-system"

for ns in $namespaces; do
  status=$(sudo kubectl get namespace "$ns" --no-headers 2>/dev/null | awk '{print $2}' || true)
  if [ "$status" != "Active" ]; then
    fail "Namespace $ns is not Active or does not exist (status: $status)"
  fi
done
success "All required namespaces are Active."

# List of pod name prefixes to check in kube-system
printf ">> Checking pod statuses in `kube-system` namespace...\n"
prefixes="cilium- cilium-operator- coredns- etcd- hubble-relay- kube-apiserver- kube-controller-manager- kube-scheduler- metallb-controller- metallb-speaker-"

for prefix in $prefixes; do
  pods_output=$(sudo kubectl get pods --all-namespaces --no-headers 2>/dev/null | awk -v p="$prefix" '$2 ~ "^"p {print $1 "|" $2 "|" $3}' || true)
  if [ -z "$pods_output" ]; then
    warn "No pods found for prefix '$prefix', continuing with other checks."
    continue
  fi

  while IFS='|' read -r ns name ready; do
    [ -z "$name" ] && continue
    IFS='/' read -r ready_num ready_total <<< "$ready"
    if [ -z "$ready_num" ] || [ -z "$ready_total" ] || [ "$ready_num" != "$ready_total" ]; then
      fail "Pod $name in namespace $ns is not fully ready ($ready)"
    fi
  done <<< "$pods_output"
done
success "All required pods in `kube-system` are Running and Ready."

# List of required services in the format namespace:service
printf ">> Checking required services...\n"
services="default:kubernetes kube-system:hubble-peer kube-system:hubble-relay kube-system:kube-dns metallb-system:metallb-webhook-service"

for svc in $services; do
  ns=$(echo "$svc" | cut -d: -f1)
  name=$(echo "$svc" | cut -d: -f2)
  if ! sudo kubectl get svc -n "$ns" "$name" --no-headers >/dev/null 2>&1; then
    fail "Service $name not found in namespace $ns"
  fi
done
success "All required services are present."

# List of required CRDs
printf ">> Checking required Custom Resource Definitions (CRDs)...\n"
crds="bfdprofiles.metallb.io bgpadvertisements.metallb.io bgppeers.metallb.io ciliumcidrgroups.cilium.io ciliumclusterwidenetworkpolicies.cilium.io ciliumendpoints.cilium.io ciliumidentities.cilium.io ciliuml2announcementpolicies.cilium.io ciliumloadbalancerippools.cilium.io ciliumnetworkpolicies.cilium.io ciliumnodeconfigs.cilium.io ciliumnodes.cilium.io ciliumpodippools.cilium.io communities.metallb.io ipaddresspools.metallb.io l2advertisements.metallb.io servicebgpstatuses.metallb.io servicel2statuses.metallb.io"

for crd in $crds; do
  if ! sudo kubectl get crd "$crd" --no-headers >/dev/null 2>&1; then
    fail "CRD $crd not found"
  fi
done
success "All required CRDs are present."

printf "%b ✅ Cluster health check passed%b\n" "$green" "$nc"
