#!/usr/bin/env bash
# Dump the CPU-path BPF maps from the bpftool DaemonSet pod on the dedicated
# noisy-node test node, for the manual runq_enqueued leak check in
# docs/latency-anomaly-investigation.md section 4.
#
#   runq_enqueued   : pid (u32) -> wakeup timestamp (u64 ns, CLOCK_MONOTONIC).
#                     A long tail of old timestamps == orphaned entries / PID
#                     reuse -> the fabricated multi-second latency reading.
#   runq_histograms : struct{cgroup_id, prev_cgroup_id, bucket} -> count (u64).
#
# Requires k8s/bpftool-daemonset.yaml deployed:
#   kubectl apply -f k8s/bpftool-daemonset.yaml
#
# Env overrides: NAMESPACE (default ebpf-noisy-neighbour), NODE_SELECTOR
# (default workload=noisy-node), KUBE_CONTEXT (default: current kube context;
# `task dump-runq-enqueued` passes the cluster's context explicitly).

set -euo pipefail

NAMESPACE="${NAMESPACE:-ebpf-noisy-neighbour}"
NODE_SELECTOR="${NODE_SELECTOR:-workload=noisy-node}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"

# Pin every kubectl call below to $KUBE_CONTEXT when set, so the dump always
# targets the intended cluster regardless of `kubectl config current-context`.
kubectl() {
  if [[ -n "${KUBE_CONTEXT}" ]]; then
    command kubectl --context "${KUBE_CONTEXT}" "$@"
  else
    command kubectl "$@"
  fi
}

echo "Finding a node matching '${NODE_SELECTOR}'..."
node="$(kubectl get nodes -l "${NODE_SELECTOR}" -o jsonpath='{.items[0].metadata.name}')"
if [[ -z "${node}" ]]; then
  echo "No node matches selector '${NODE_SELECTOR}'." >&2
  exit 1
fi
echo "Test node: ${node}"

pod="$(kubectl get pods -n "${NAMESPACE}" -l name=bpftool \
  --field-selector "spec.nodeName=${node}" \
  -o jsonpath='{.items[0].metadata.name}')"
if [[ -z "${pod}" ]]; then
  echo "No bpftool pod on ${node} in namespace ${NAMESPACE}." >&2
  echo "Deploy it with: kubectl apply -f k8s/bpftool-daemonset.yaml" >&2
  exit 1
fi
echo "bpftool pod: ${pod}"
echo

# Entry count first — the single number that tells you whether the map is
# leaking. bpftool --json keeps this robust regardless of map size.
if count="$(kubectl exec -n "${NAMESPACE}" "${pod}" -- \
  bpftool --json map dump name runq_enqueued 2>/dev/null | jq 'length')"; then
  echo "runq_enqueued entry count: ${count}"
else
  echo "runq_enqueued entry count: (unavailable — is the eBPF DaemonSet loaded on ${node}?)"
fi
echo

for map_name in runq_enqueued runq_histograms; do
  echo "=================================================================="
  echo "bpftool map dump name ${map_name}"
  echo "=================================================================="
  kubectl exec -n "${NAMESPACE}" "${pod}" -- bpftool map dump name "${map_name}" \
    || echo "(dump of ${map_name} failed — is the eBPF DaemonSet loaded on ${node}?)" >&2
  echo
done

cat <<'EOF'
Reading the dump:
  - runq_enqueued values are CLOCK_MONOTONIC nanoseconds. Compare against the
    node's "now": kubectl exec -n <ns> <pod> -- cat /proc/uptime  (field 1, s).
  - Leak confirmed: entry count keeps climbing across a run and a tail of
    entries is many seconds / minutes old. Cross-check with the collector's
    ebpf_runq_enqueued_entries / ebpf_runq_enqueued_stale_entries gauges.
EOF
