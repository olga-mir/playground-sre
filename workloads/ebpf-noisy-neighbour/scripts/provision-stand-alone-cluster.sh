#!/bin/bash
set -euo pipefail

# This script provisions a standalone GKE cluster with a tainted nodepool,
# for manual testing outside of the GitOps (Flux) setup in the `playground`
# repo. Requires environment variables: PROJECT_ID, CLUSTER_LOCATION, CLUSTER_NAME.
# Safe to re-run: an existing cluster or test-pool is left as-is, then
# credentials are refreshed and the metrics-server CPU limit is patched again.

PROJECT_ID="${PROJECT_ID:?'PROJECT_ID environment variable is required'}"
CLUSTER_LOCATION="${CLUSTER_LOCATION:?'CLUSTER_LOCATION environment variable is required (a zone, e.g. australia-southeast1-a)'}"
CLUSTER_NAME="${CLUSTER_NAME:?'CLUSTER_NAME environment variable is required'}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKLOAD_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

echo "Configuring gcloud for project ${PROJECT_ID}..."
gcloud config set project "${PROJECT_ID}"

# Create a zonal cluster. Regular-channel GKE turns NodeLocal DNS on unless
# NodeLocalDNS=DISABLED is set. --addons replaces the addon set, so the other
# entries keep the defaults this cluster actually uses.
# --enable-dataplane-v2 selects Cilium and does not install kube-proxy. The
# anetd DaemonSet still has to fit on the shared core; that budget is not
# set here. An existing cluster is not updated; delete it first to pick up
# flag changes.
if gcloud container clusters describe "${CLUSTER_NAME}" --zone "${CLUSTER_LOCATION}" >/dev/null 2>&1; then
  echo "Cluster ${CLUSTER_NAME} already exists in ${CLUSTER_LOCATION}, skipping create."
else
  echo "Creating GKE cluster ${CLUSTER_NAME} in ${CLUSTER_LOCATION}..."
  gcloud container clusters create "${CLUSTER_NAME}" \
      --zone "${CLUSTER_LOCATION}" \
      --num-nodes 1 \
      --release-channel "regular" \
      --workload-pool="${PROJECT_ID}.svc.id.goog" \
      --enable-managed-prometheus \
      --enable-ip-alias \
      --enable-dataplane-v2 \
      --machine-type "e2-medium" \
      --disk-type="pd-standard" \
      --disk-size=50 \
      --enable-autoscaling --min-nodes=1 --max-nodes=5 \
      --addons=HttpLoadBalancing=ENABLED,HorizontalPodAutoscaling=ENABLED,GcePersistentDiskCsiDriver=ENABLED,NodeLocalDNS=DISABLED
fi

# e2-standard-2 (2 vCPU). Taint + label so only our test workloads land here.
# --system-config-from-file turns on kubelet cpuManagerPolicy: static, which lets
# the ballast DaemonSet (k8s/ballast.yaml) claim one exclusive core per node and
# confine the noisy-neighbour experiment to the remaining shared CPU.
# Autoscaling stays off: a second node would let bully/victim land on different
# nodes, defeating the point of a shared-core contention experiment.
if gcloud container node-pools describe "test-pool" --cluster "${CLUSTER_NAME}" --zone "${CLUSTER_LOCATION}" >/dev/null 2>&1; then
  echo "Node pool test-pool already exists, skipping create."
else
  echo "Creating tainted nodepool for testing (2 vCPU, static CPU Manager)..."
  gcloud container node-pools create "test-pool" \
      --cluster="${CLUSTER_NAME}" \
      --zone="${CLUSTER_LOCATION}" \
      --machine-type="e2-standard-2" \
      --num-nodes=1 \
      --node-taints="dedicated=noisy-node:NoSchedule" \
      --node-labels="workload=noisy-node" \
      --system-config-from-file="${WORKLOAD_DIR}/gke-node-system-config.yaml" \
      --no-enable-autoscaling \
      --disk-type="pd-standard" \
      --disk-size=50
fi

echo "Fetching cluster credentials..."
gcloud container clusters get-credentials "${CLUSTER_NAME}" --zone="${CLUSTER_LOCATION}"

# GKE has no clusters-create flag for metrics-server resources. It sets CPU
# request=limit around 40m, so kubelet scrapes burst over the CFS quota and
# the container is throttled while the node is idle. Patch the limit after
# the addon Deployment exists. EnsureExists does not stick here: GKE writes
# addonmanager.kubernetes.io/mode=Reconcile back within seconds, and leaves
# the CPU limit in place. A GKE upgrade that recreates metrics-server-v<version>
# drops this patch. kube-system/metrics-server-config is not consulted: the
# pod ships without the addon-resizer nanny that used to read baseCPU.
KUBE_CONTEXT="gke_${PROJECT_ID}_${CLUSTER_LOCATION}_${CLUSTER_NAME}"

# Curated cAdvisor collection omits kubelet PSI counters. The addon
# ClusterNodeMonitoring is Reconcile-managed, so this separate object keeps
# the six container_pressure_* series. Safe to re-apply.
echo "Applying PSI metrics collector..."
kubectl --context "${KUBE_CONTEXT}" apply -f "${WORKLOAD_DIR}/k8s/psi-collector.yaml"

METRICS_SERVER_CPU_LIMIT="500m"
echo "Raising metrics-server CPU limit to ${METRICS_SERVER_CPU_LIMIT}..."
# /bin/bash on macOS is 3.2 and has no mapfile.
deadline=$((SECONDS + 180))
deploy=""
while (( SECONDS < deadline )); do
  names="$(kubectl --context "${KUBE_CONTEXT}" -n kube-system get deploy -l k8s-app=metrics-server -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true)"
  deploy="${names%%$'\n'*}"
  if [[ -n "${deploy}" ]]; then
    break
  fi
  sleep 5
done
if [[ -z "${deploy}" ]]; then
  echo "metrics-server deployment did not appear within 3 minutes." >&2
  exit 1
fi
kubectl --context "${KUBE_CONTEXT}" -n kube-system patch deployment "${deploy}" --type strategic --patch "$(cat <<EOF
{
  "spec": {
    "template": {
      "spec": {
        "containers": [
          {
            "name": "metrics-server",
            "resources": {
              "limits": {
                "cpu": "${METRICS_SERVER_CPU_LIMIT}"
              }
            }
          }
        ]
      }
    }
  }
}
EOF
)"
echo "metrics-server (${deploy}) CPU limit set to ${METRICS_SERVER_CPU_LIMIT}."

echo "Cluster ${CLUSTER_NAME} provisioned successfully."
echo "Next: task ebpf:local-build-image && task ebpf:deploy-stack"
