#!/usr/bin/env bash
# List containers on the noisy-node pool by the CPU the kubelet pinned them to.
#
# Static CPU Manager writes cpuset.cpus on each container cgroup. This reads
# that file from the host cgroup mount in the ebpf-ds pod and joins it with
# pod names. A Cloud Monitoring widget cannot show this: cpuset assignment is
# not a metric.
#
# Env overrides: NAMESPACE (default ebpf-noisy-neighbour), NODE_SELECTOR
# (default workload=noisy-node), KUBE_CONTEXT (default: current kube context;
# `task ebpf:list-cpu-pinning` passes the cluster's context explicitly).

set -euo pipefail

NAMESPACE="${NAMESPACE:-ebpf-noisy-neighbour}"
NODE_SELECTOR="${NODE_SELECTOR:-workload=noisy-node}"
KUBE_CONTEXT="${KUBE_CONTEXT:-}"

kubectl() {
  if [[ -n "${KUBE_CONTEXT}" ]]; then
    command kubectl --context "${KUBE_CONTEXT}" "$@"
  else
    command kubectl "$@"
  fi
}

first_name() {
  local out
  if ! out="$("$@")"; then
    echo "kubectl failed: $*" >&2
    exit 1
  fi
  printf '%s\n' "${out%%$'\n'*}"
}

if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required to join cpuset data with pod names." >&2
  exit 1
fi

nodes="$(kubectl get nodes -l "${NODE_SELECTOR}" -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')"
if [[ -z "${nodes}" ]]; then
  echo "No node matches selector '${NODE_SELECTOR}'." >&2
  exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

# Walk container scopes under kubepods and the top-level host cgroups.
# Printed from inside the node, so the shell is the image's /bin/sh.
read -r -d '' REMOTE <<'EOF' || true
set -eu
echo "---containers---"
find /sys/fs/cgroup/kubepods.slice -type d -name '*.scope' 2>/dev/null | while read -r dir; do
  parent="$(basename "$(dirname "$dir")")"
  uid="$(printf '%s\n' "$parent" | sed -n 's/.*pod\([0-9a-f_][0-9a-f_]*\)\.slice$/\1/p' | tr '_' '-')"
  cid="$(basename "$dir" | sed -n 's/^cri-containerd-\(.*\)\.scope$/\1/p')"
  if [ -z "$uid" ] || [ -z "$cid" ]; then
    continue
  fi
  cpus="$(tr -d '[:space:]' < "$dir/cpuset.cpus" 2>/dev/null || true)"
  eff="$(tr -d '[:space:]' < "$dir/cpuset.cpus.effective" 2>/dev/null || true)"
  printf '%s\t%s\t%s\t%s\n' "$cpus" "$eff" "$uid" "$cid"
done
echo "---host---"
for dir in /sys/fs/cgroup/*/; do
  [ -f "${dir}cpuset.cpus.effective" ] || continue
  name="$(basename "$dir")"
  cpus="$(tr -d '[:space:]' < "${dir}cpuset.cpus" 2>/dev/null || true)"
  eff="$(tr -d '[:space:]' < "${dir}cpuset.cpus.effective" 2>/dev/null || true)"
  printf '%s\t%s\t%s\n' "$name" "$cpus" "$eff"
done
EOF

while IFS= read -r node; do
  [[ -z "${node}" ]] && continue
  pod="$(first_name kubectl get pods -n "${NAMESPACE}" -l app=ebpf-ds \
    --field-selector "spec.nodeName=${node}" \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}')"
  if [[ -z "${pod}" ]]; then
    echo "No ebpf-ds pod on ${node}. Deploy it with: task ebpf:deploy-k8s" >&2
    exit 1
  fi
  if ! kubectl exec -n "${NAMESPACE}" "${pod}" -- sh -c "${REMOTE}" >"${tmpdir}/pins-${node}"; then
    echo "Failed to read cgroups on ${node} via ${pod}." >&2
    exit 1
  fi
done <<<"${nodes}"

kubectl get pods -A -o json >"${tmpdir}/pods.json"

python3 - "${tmpdir}" <<'PY'
import json, sys
from pathlib import Path

root = Path(sys.argv[1])
pods = json.loads((root / "pods.json").read_text())

by_cid = {}
by_uid = {}
for pod in pods["items"]:
    meta = pod["metadata"]
    spec = pod["spec"]
    by_uid[meta["uid"]] = {
        "ns": meta["namespace"],
        "pod": meta["name"],
        "node": spec.get("nodeName") or "",
    }
    status = pod.get("status") or {}
    containers = {c["name"]: c for c in spec.get("containers", [])}
    containers.update({c["name"]: c for c in spec.get("initContainers", [])})
    statuses = list(status.get("containerStatuses") or [])
    statuses += list(status.get("initContainerStatuses") or [])
    for st in statuses:
        cid = (st.get("containerID") or "").split("://", 1)[-1]
        if not cid:
            continue
        res = (containers.get(st["name"]) or {}).get("resources") or {}
        req = ((res.get("requests") or {}).get("cpu")) or "0"
        by_cid[cid] = {
            "ns": meta["namespace"],
            "pod": meta["name"],
            "container": st["name"],
            "request": req,
            "node": spec.get("nodeName") or "",
        }

def milli(req):
    if req.endswith("m"):
        return int(req[:-1])
    return int(float(req) * 1000)

def fmt_milli(m):
    if m % 1000 == 0:
        return f"{m // 1000}"
    return f"{m}m"

for pin_path in sorted(root.glob("pins-*")):
    node = pin_path.name[len("pins-"):]
    text = pin_path.read_text()
    if "---containers---" not in text or "---host---" not in text:
        sys.exit(f"unexpected cgroup dump from {node}")
    containers_txt, host_txt = text.split("---host---", 1)
    containers_txt = containers_txt.split("---containers---", 1)[1]
    groups = {}
    unpinned = []
    for line in containers_txt.splitlines():
        if not line.strip():
            continue
        cpus, eff, uid, cid = line.split("\t")
        info = by_cid.get(cid)
        if info is None:
            owner = by_uid.get(uid) or {}
            info = {
                "ns": owner.get("ns", "?"),
                "pod": owner.get("pod", uid[:8] + "…"),
                "container": "sandbox",
                "request": "0",
                "node": owner.get("node", node),
            }
        row = (info["ns"], info["pod"], info["container"], info["request"])
        if cpus:
            groups.setdefault(cpus, []).append(row)
        else:
            unpinned.append(row + (eff,))

    print(f"node {node}")
    if not groups and not unpinned:
        print("  (no container cgroups)")
    def cpu_key(s):
        parts = []
        for tok in s.replace("-", ",").split(","):
            parts.append(int(tok) if tok.isdigit() else 1_000_000)
        return parts

    for cpus in sorted(groups, key=cpu_key):
        rows = sorted(groups[cpus])
        total = sum(milli(r[3]) for r in rows)
        print(f"  CPU {cpus}  requests {fmt_milli(total)}")
        for ns, pod, container, req in rows:
            print(f"    {req:>6}  {ns}/{pod}/{container}")
    if unpinned:
        print("  unpinned (empty cpuset, inherits the parent effective set)")
        for ns, pod, container, req, eff in sorted(unpinned):
            print(f"    {req:>6}  {ns}/{pod}/{container}  effective={eff or '?'}")
    print("  host cgroups")
    for line in host_txt.splitlines():
        if not line.strip():
            continue
        name, cpus, eff = line.split("\t")
        shown = cpus or f"(empty, effective {eff or '?'})"
        print(f"    {name:<28} {shown}")
    print()
PY
