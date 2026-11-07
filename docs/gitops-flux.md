# GitOps with Flux

Workloads in this repo are deployed to the `apps-dev` GKE cluster by FluxCD. The Flux wiring (GitRepository, Kustomizations, image automation) lives in the platform repo [`olga-mir/playground`](https://github.com/olga-mir/playground); this repo only holds the manifests and the CI that builds images.

| Workload | Flux Kustomization path | Namespace | Image |
|---|---|---|---|
| perf-lab | `./workloads/perf-lab/k8s` | `sre` | `olmigar/perf-lab` |
| ebpf-noisy-neighbour | `./workloads/ebpf-noisy-neighbour/k8s` | `ebpf-noisy-neighbour` | `olmigar/experiment-ebpf` |

## Flow

```
merge to main → CI builds + pushes image → Flux image-reflector sees new tag
  → ImageUpdateAutomation commits the new tag to k8s/deployment.yaml → Flux reconciles → pods roll
```

- CI: `.github/workflows/build-push.yml` (perf-lab) and `build-push-ebpf.yml` (ebpf-noisy-neighbour). Both push to Docker Hub using `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` repo secrets.
- Tag format: `main-<YYYYMMDDHHMMSS>-<short-sha>`. The `ImagePolicy` sorts numerically on the timestamp.
- Promotion latency is ~5 minutes (reconcile intervals).
- Why Docker Hub rather than Artifact Registry: see [flux-image-automation-tradeoff.md](flux-image-automation-tradeoff.md).
- Experiment-time workloads (bully, victim, load generators) are deliberately not part of the GitOps Kustomization. See the ebpf-noisy-neighbour README/Taskfile for the manual path.

## Prerequisite on the platform cluster

An SSH deploy key secret named `playground-sre-deploy-key` in `flux-system`, with write access to this repo. Image automation needs it to commit tag updates back.

## Using your own fork

Assumes Flux is already bootstrapped. Replace `you` below and update the image references in `workloads/<name>/k8s/*.yaml` and `.github/workflows/build-push*.yml`.

### 1. Source and Kustomization

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: playground-sre
  namespace: flux-system
spec:
  interval: 1m
  url: https://github.com/you/playground-sre
  ref:
    branch: main
  secretRef:
    name: playground-sre-deploy-key
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  interval: 5m
  sourceRef:
    kind: GitRepository
    name: playground-sre
  path: ./workloads/perf-lab/k8s
  prune: true
  targetNamespace: sre
```

### 2. Image automation

```yaml
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImageRepository
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  image: docker.io/you/perf-lab
  interval: 1m
---
apiVersion: image.toolkit.fluxcd.io/v1beta2
kind: ImagePolicy
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  imageRepositoryRef:
    name: perf-lab
  filterTags:
    pattern: '^main-(?P<ts>[0-9]+)-[a-f0-9]+$'
    extract: '$ts'
  policy:
    numerical:
      order: asc
---
apiVersion: image.toolkit.fluxcd.io/v1beta1
kind: ImageUpdateAutomation
metadata:
  name: perf-lab
  namespace: flux-system
spec:
  interval: 5m
  sourceRef:
    kind: GitRepository
    name: playground-sre
  git:
    checkout:
      ref:
        branch: main
    commit:
      author:
        name: fluxcdbot
        email: fluxcdbot@users.noreply.github.com
      messageTemplate: 'chore: update perf-lab image to {{range .Updated.Images}}{{.}}{{end}}'
    push:
      branch: main
  update:
    strategy: Setters
    path: ./workloads/perf-lab/k8s
```

### 3. Deploy key

```bash
ssh-keygen -t ed25519 -C "flux-image-automation" -f /tmp/deploy-key -N ""

# Add the public key to your fork: Settings → Deploy keys (check "Allow write access")
cat /tmp/deploy-key.pub

kubectl create secret generic playground-sre-deploy-key \
  --from-file=identity=/tmp/deploy-key \
  --from-file=identity.pub=/tmp/deploy-key.pub \
  --from-literal=known_hosts="$(ssh-keyscan github.com)" \
  -n flux-system
```
