#!/usr/bin/env bash
# List all Kubernetes pods grouped by Node, alongside their eBPF cgroup format:
# pod/<pod-uid-first-8-chars>/<container-id-first-8-chars>

set -euo pipefail

# Pin every kubectl call to an explicit context when the caller sets one
# (`task list-pod-cgroups` passes KUBE_CONTEXT); fall back to the current
# context for standalone use.
KUBE_CONTEXT="${KUBE_CONTEXT:-}"
kubectl() {
  if [[ -n "${KUBE_CONTEXT}" ]]; then
    command kubectl --context "${KUBE_CONTEXT}" "$@"
  else
    command kubectl "$@"
  fi
}

echo "Fetching pods across all namespaces..."
echo ""

(
  printf "NODE\tNAMESPACE\tPOD\tCONTAINER\tEBPF LABEL\n"
  kubectl get pods -A -o json | jq -r '
    .items[] |
    .spec.nodeName as $node |
    .metadata.namespace as $ns |
    .metadata.name as $pod |
    (.metadata.uid | gsub("_"; "-") | .[0:8]) as $shortUid |
    (.status.containerStatuses // [])[] |
    .name as $cName |
    (.containerID // "") as $cIDFull |
    (($cIDFull | sub("^.+://"; "") | sub("^cri-containerd-"; "") | sub("^docker-"; "") | .[0:8])) as $shortCid |
    "\(($node | sub("^gke-[a-z0-9-]+-cluster-"; "")))\t\($ns)\t\($pod)\t\($cName)\tpod/\($shortUid)/\($shortCid)"
  ' | sort
) | column -t -s $'\t'
