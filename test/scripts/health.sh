#!/usr/bin/env bash
# File: `test/scripts/cluster-health.sh`
# Modular cluster health checks (non-destructive). Returns non-zero on failure.

set -euo pipefail

green="\033[0;32m"
red="\033[0;31m"
yellow="\033[0;33m"
nc="\033[0m"

success() { printf "%b ✅ %s%b\n" "$green" "$1" "$nc"; }
fail()    { printf "%b ❌ %s%b\n" "$red" "$1" "$nc"; exit 1; }
warn()    { printf "%b ⚠️ %s%b\n" "$yellow" "$1" "$nc"; }

KUBECTL="${KUBECTL:-kubectl}"

check_cluster_access() {
  printf ">> Checking cluster access...\n"
  if $KUBECTL get nodes >/dev/null 2>&1; then
    success "Cluster access verified."
    return 0
  fi
  fail "Cannot access the cluster with kubectl."
}

check_control_plane() {
  printf ">> Checking control plane endpoints...\n"
  ok=0
  if $KUBECTL get --raw="/healthz" >/dev/null 2>&1; then
    success "API server /healthz OK."
    ok=$((ok+1))
  else
    warn "API server /healthz failed."
  fi

  if $KUBECTL get --raw="/readyz" >/dev/null 2>&1; then
    success "API server /readyz OK."
    ok=$((ok+1))
  else
    warn "API server /readyz failed."
  fi

  # check kube-system control plane pods (apiserver/controller-manager/scheduler) if present
  pods=$($KUBECTL get pods -n kube-system --no-headers 2>/dev/null | awk '{print $2 "|" $1 "|" $3}')
  found=0
  while IFS='|' read -r ready name status; do
    found=1
    IFS='/' read -r ready_num ready_total <<<"$ready"
    if [ -z "$ready_num" ] || [ -z "$ready_total" ] || [ "$ready_num" != "$ready_total" ]; then
      warn "Control-plane related pod $name not fully ready ($ready) status=$status"
    fi
  done <<<"$pods"
  if [ "$found" -eq 1 ]; then
    success "Control-plane pods checked."
  else
    warn "No control-plane pods discovered in kube-system (may be managed externally)."
  fi
  return 0
}

check_nodes_ready() {
  printf ">> Checking node statuses...\n"
  not_ready=$($KUBECTL get nodes --no-headers 2>/dev/null | awk '{print $2}' | grep -v '^Ready$' || true)
  if [ -n "$not_ready" ]; then
    fail "Some nodes are not Ready: $not_ready"
  fi
  success "All nodes are Ready."
}

check_namespaces() {
  printf ">> Verifying required namespaces...\n"
  namespaces="default kube-system kube-public kube-node-lease"
  for ns in $namespaces; do
    status=$($KUBECTL get namespace "$ns" --no-headers 2>/dev/null | awk '{print $2}' || true)
    if [ "$status" != "Active" ]; then
      fail "Namespace $ns is not Active or does not exist (status: $status)"
    fi
  done
  success "Required namespaces are Active."
}

check_critical_pods() {
  printf ">> Checking critical pods readiness (all namespaces)\n"
  prefixes="coredns- kube-apiserver- kube-controller-manager- kube-scheduler- cilium- metallb-"
  for prefix in $prefixes; do
    # --all-namespaces output: NAMESPACE NAME READY STATUS RESTARTS AGE
    pods_output=$($KUBECTL get pods --all-namespaces --no-headers 2>/dev/null | awk -v p="$prefix" '$2 ~ "^"p {print $1 "|" $2 "|" $3 "|" $4}' || true)
    if [ -z "$pods_output" ]; then
      warn "No pods found for prefix '$prefix'; skipping."
      continue
    fi
    while IFS='|' read -r ns name ready status; do
      [ -z "$name" ] && continue
      IFS='/' read -r ready_num ready_total <<<"$ready"
      if [ -z "$ready_num" ] || [ -z "$ready_total" ] || [ "$ready_num" != "$ready_total" ]; then
        fail "Pod $name in namespace $ns is not fully ready ($ready) status=$status"
      fi
    done <<<"$pods_output"
  done
  success "Critical pods are Running and Ready."
}

check_services() {
  printf ">> Checking required services...\n"
  services="default:kubernetes kube-system:kube-dns metallb-system:metallb-webhook-service"
  for svc in $services; do
    ns=${svc%%:*}
    name=${svc##*:}
    if ! $KUBECTL get svc -n "$ns" "$name" --no-headers >/dev/null 2>&1; then
      fail "Service $name not found in namespace $ns"
    fi
  done
  success "Required services present."
}

check_crds() {
  printf ">> Checking CRDs (sample list)...\n"
  crds="bgppeers.metallb.io ciliumendpoints.cilium.io"
  for crd in $crds; do
    if ! $KUBECTL get crd "$crd" --no-headers >/dev/null 2>&1; then
      warn "CRD $crd not found; may be optional"
    fi
  done
  success "CRD presence checked (warnings may indicate optional features)."
}

check_storage() {
  printf ">> Checking storage classes and CSI pods...\n"
  scs=$($KUBECTL get storageclass --no-headers 2>/dev/null | awk '{print $1}' || true)
  if [ -z "$scs" ]; then
    warn "No StorageClass found."
  else
    success "Found StorageClass(s): ${scs}"
  fi

  # quick check for CSI pods in kube-system
  csi_pods=$($KUBECTL get pods -n kube-system --no-headers 2>/dev/null | grep -E 'csi|provisioner' || true)
  if [ -z "$csi_pods" ]; then
    warn "No CSI pods detected in kube-system (cluster may use in-tree or not provide dynamic provisioning)."
  else
    success "CSI-related pods present in kube-system."
  fi
}

check_dns_resolution() {
  printf ">> Checking cluster DNS resolution via a transient pod...\n"
  if ! $KUBECTL run dns-test --restart=Never --rm -i --image=busybox -- nslookup kubernetes.default.svc.cluster.local >/dev/null 2>&1; then
    warn "DNS lookup for kubernetes.default.svc.cluster.local failed (via busybox)."
  else
    success "Cluster DNS resolution appears functional."
  fi
}

check_loadbalancer() {
  printf ">> Checking LoadBalancer services for external IPs (if any)...\n"
  lbs=$($KUBECTL get svc --all-namespaces -o jsonpath='{range .items[?(@.spec.type=="LoadBalancer")]}{.metadata.namespace}:{.metadata.name} {.status.loadBalancer.ingress[0].ip}{"\n"}{end}' 2>/dev/null || true)
  if [ -z "$lbs" ]; then
    warn "No LoadBalancer services found; cluster may not use external LBs."
  else
    echo "$lbs" | while read -r line; do
      ns_name=$(awk '{print $1}' <<<"$line")
      ip=$(awk '{print $2}' <<<"$line")
      if [ -z "$ip" ] || [ "$ip" = "null" ]; then
        warn "LoadBalancer $ns_name has no external IP yet: $line"
      else
        success "LoadBalancer $ns_name assigned external IP $ip"
      fi
    done
  fi
}

# --- New: listing and lightweight validation functions ---

list_basic_resources() {
  printf ">> Listing summary of key resources (pods, svc, cm, ns)\n"

  printf "\n-- Pods (all namespaces, top 50 lines) --\n"
  $KUBECTL get pods --all-namespaces | sed -n '1,50p' || true

  printf "\n-- Services (all namespaces) --\n"
  $KUBECTL get svc --all-namespaces || true

  printf "\n-- ConfigMaps (all namespaces) --\n"
  $KUBECTL get cm --all-namespaces || true

  printf "\n-- Namespaces --\n"
  $KUBECTL get ns --all-namespaces || true
  printf "\n"
}

# Check for presence of important items (based on the provided cluster data).
check_expected_resources() {
  printf ">> Validating presence of expected pods/services/configmaps/namespaces\n"

  # expected pod prefixes (search across all namespaces)
  expected_pod_prefixes=(
    "cilium-" "cilium-operator-" "coredns-" "etcd-" "hubble-relay-" "kube-apiserver-"
    "kube-controller-manager-" "kube-scheduler-" "metallb-controller-" "metallb-speaker"
  )

  missing_pod_prefixes=()
  for p in "${expected_pod_prefixes[@]}"; do
    if ! $KUBECTL get pods --all-namespaces --no-headers 2>/dev/null | awk '{print $2}' | grep -E "^$p" >/dev/null 2>&1; then
      missing_pod_prefixes+=("$p")
    fi
  done
  if [ ${#missing_pod_prefixes[@]} -gt 0 ]; then
    warn "Some expected pod prefixes not found: ${missing_pod_prefixes[*]}"
  else
    success "All expected pod prefixes found."
  fi

  # expected services namespace:name
  expected_services=(
    "default:kubernetes" "kube-system:kube-dns" "kube-system:hubble-peer" "kube-system:hubble-relay" "metallb-system:metallb-webhook-service"
  )
  missing_services=()
  for s in "${expected_services[@]}"; do
    ns=${s%%:*}
    name=${s##*:}
    if ! $KUBECTL get svc -n "$ns" "$name" --no-headers >/dev/null 2>&1; then
      missing_services+=("$s")
    fi
  done
  if [ ${#missing_services[@]} -gt 0 ]; then
    warn "Some expected services missing: ${missing_services[*]}"
  else
    success "All expected services present."
  fi

  # expected namespaces
  expected_namespaces=("default" "kube-system" "kube-public" "kube-node-lease" "metallb-system" "cilium-secrets")
  missing_ns=()
  for ns in "${expected_namespaces[@]}"; do
    status=$($KUBECTL get ns "$ns" --no-headers 2>/dev/null | awk '{print $2}' || true)
    if [ "$status" != "Active" ]; then
      missing_ns+=("$ns")
    fi
  done
  if [ ${#missing_ns[@]} -gt 0 ]; then
    warn "Some expected namespaces missing or not Active: ${missing_ns[*]}"
  else
    success "All expected namespaces Active."
  fi

  # expected configmaps (namespace:name)
  expected_configmaps=(
    "kube-system:cilium-config" "kube-system:coredns" "kube-system:extension-apiserver-authentication"
    "kube-system:kubeadm-config" "kube-system:kubelet-config" "metallb-system:metallb-excludel2"
    "kube-public:cluster-info"
  )
  missing_cms=()
  for cm in "${expected_configmaps[@]}"; do
    ns=${cm%%:*}
    name=${cm##*:}
    if ! $KUBECTL get cm -n "$ns" "$name" --no-headers >/dev/null 2>&1; then
      missing_cms+=("$cm")
    fi
  done
  if [ ${#missing_cms[@]} -gt 0 ]; then
    warn "Some expected ConfigMaps missing: ${missing_cms[*]}"
  else
    success "All expected ConfigMaps present."
  fi
}

main() {
  check_cluster_access
  check_control_plane
  check_nodes_ready
  check_namespaces
  check_critical_pods
  check_services
  check_crds
  check_storage
  check_dns_resolution
  check_loadbalancer

  # New: list and lightweight validation
  list_basic_resources
  check_expected_resources

  printf "%b ✅ Cluster health checks completed%b\n" "$green" "$nc"
}

main "$@"
