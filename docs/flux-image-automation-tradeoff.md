# Decision: Flux image automation vs. GCP project ID leaking into git

Status: **resolved 2026-09-20 — chose Option B (Docker Hub)**

## Decision

Went with **Option B**: reverted `perf-lab` and `experiment-ebpf` to Docker Hub
(`olmigar/perf-lab`, `olmigar/experiment-ebpf`), keeping Flux's native
`ImageRepository`/`ImagePolicy`/`ImageUpdateAutomation` loop working exactly as designed. The
deciding factor was that Flux's own image-promotion automation — not just CI building and
deploying — was the point of the original setup, and Option A would have given that up.

Docker Hub's stored, expiring token (the original problem that motivated the Artifact Registry
migration in the first place) is accepted as the trade-off. The token is being rotated now
(GitHub Actions `DOCKERHUB_TOKEN` secret); no expiry-avoidance work (e.g. a no-expiry token) was
in scope for this pass.

**What changed to implement this:**
- `playground-sre`: `workloads/perf-lab/k8s/deployment.yaml` and
  `workloads/ebpf-noisy-neighbour/k8s/daemonset.yaml` — image refs back to
  `index.docker.io/olmigar/...`, no `${REGION}`/`${PROJECT_ID}` placeholders.
- `playground-sre`: `.github/workflows/build-push.yml` /
  `build-push-ebpf.yml` — back to `docker/login-action` with
  `DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN`; GCP WIF auth step removed.
- `playground-sre`: `AGENTS.md` updated to match (image refs, GitOps table, CI paragraph).
- `playground`: `kubernetes/tenants/base/sre/image-repository.yaml` and
  `kubernetes/tenants/base/sre-ebpf/image-repository.yaml` — `spec.image` back to
  `index.docker.io/olmigar/...`, `provider: gcp` removed (defaults to generic/Docker Hub).

**Not touched, and why:** `workloads/perf-lab/k8s/serviceaccount.yaml` still uses
`${PROJECT_ID}` for its `iam.gke.io/gcp-service-account` Workload Identity annotation — that's
resolved once by the `sre` Flux Kustomization's `postBuild.substituteFrom` (`platform-config`),
same as the rest of the `playground` repo, and is never touched by `ImageUpdateAutomation`. It
carries none of the leak risk described below, so it was left as-is. The out-of-band
`kubectl annotate serviceaccount image-reflector-controller ... iam.gke.io/gcp-service-account=`
binding (for AR access) was never committed to git and is now moot; if it was ever applied to a
live `apps-dev`, it can be removed next time that cluster is up, but there's nothing to clean up
in either repo.

## The original problem

Before this decision, `workloads/perf-lab/k8s/deployment.yaml` and
`workloads/ebpf-noisy-neighbour/k8s/daemonset.yaml` referenced images as:

```yaml
image: ${REGION}-docker.pkg.dev/${PROJECT_ID}/perf-lab/perf-lab:main-...  # {"$imagepolicy": "flux-system:perf-lab"}
```

`${REGION}`/`${PROJECT_ID}` are meant to be resolved only in-cluster, via Flux's
`postBuild.substituteFrom` against the `platform-config` ConfigMap (never committed to git —
see `playground` repo's `bootstrap/bootstrap-control-plane-cluster.sh`). This is the same
pattern the rest of the `playground` repo already uses to keep the GCP project ID
(`<the real project id>`) out of a public repo.

**This pattern does not survive Flux's `ImageUpdateAutomation`.** Traced through the actual
mechanism (fluxcd/image-automation-controller and fluxcd/image-reflector-controller docs):

1. `ImageRepository.spec.image` (in `playground` repo, `kubernetes/tenants/base/sre*/image-repository.yaml`)
   is itself applied through a Flux `Kustomization` with `postBuild.substituteFrom` — so the
   **live, in-cluster** object holds the fully resolved path
   (`<region>-docker.pkg.dev/<project-id>/perf-lab/perf-lab`), not the placeholder.
2. `ImagePolicy.status.latestRef.image` is documented as "derived from the ImageRepository
   image" — i.e. it inherits that same fully-resolved, real path.
3. For a plain Kubernetes `Deployment`/`DaemonSet` `image:` field, Flux's docs are explicit
   that the `$imagepolicy` setter marker (no field suffix, unlike the 3-way split available to
   Helm `values.yaml`) replaces the **entire** field — not just the tag — with
   `ImagePolicy.status.latestRef.image` + the selected tag.
4. `ImageUpdateAutomation` then commits that fully-resolved string straight into git, in both
   the file content and (via `messageTemplate`'s `.Changed.Changes[].OldValue`/`.NewValue`) the
   commit message.

Net effect: the **first** successful automated tag bump after `apps-dev` reconciles would
overwrite the `${PROJECT_ID}` placeholder in `deployment.yaml`/`daemonset.yaml` with the literal
`<the real project id>`, and commit it — to a public repo. This is inherent to how `ImageUpdateAutomation`
resolves values; it is not something `messageTemplate` customization can fix, since the template
only controls commit-message wording, not what gets written into the file itself.

This is also not specific to the repo being private/public on the registry side: Artifact
Registry image paths always embed the project ID structurally
(`<region>-docker.pkg.dev/<project-id>/<repo>/<image>`), unlike Docker Hub
(`<org>/<image>`, no cloud project reference). So *any* AR image reference that Flux's
image automation writes back to git will contain the project ID — there's no placeholder-safe
way to let Flux do it.

**Outcome:** this never actually happened — `apps-dev` wasn't running while the AR-based
`ImageRepository`/`ImagePolicy`/`ImageUpdateAutomation` config was live, so
`ImageUpdateAutomation` never reconciled against these files, and the placeholders were never
overwritten. The risk was latent and was closed off by the decision above (Option B) before
`apps-dev` came back up.

## Options

### A. Move the tag-bump into CI, forgo Flux's image automation for these workloads

The GitHub Actions workflows (`build-push.yml`, `build-push-ebpf.yml`) already compute the new
tag. Add a step that edits only the tag substring of `deployment.yaml`/`daemonset.yaml`
(sed/yq, matched on the `$imagepolicy` marker line), leaving `${REGION}`/`${PROJECT_ID}`
untouched, then commits and pushes back to `main`. Remove the now-pointless
`ImageRepository`/`ImagePolicy`/`ImageUpdateAutomation` CRs for `perf-lab` and
`experiment-ebpf` from the `playground` repo.

- Keeps: private Artifact Registry, WIF (no stored/expiring credential), project ID out of git.
- Loses: Flux's independent "detect new registry tag and deploy" reconcile loop — CI becomes
  responsible for both building the image and committing the deploy change, in one workflow run.
  (In practice these already happen back-to-back on every push to `main`, so the functional gap
  is small — mainly it removes the ability for something *other than this CI pipeline* to push a
  new tag and have it auto-deployed.)

### B. Revert to Docker Hub, rotate the token periodically

Docker Hub image references never contain a GCP project ID, so this whole class of leak doesn't
exist there. Keeps Flux's native image automation loop working exactly as designed. Trade-off:
back to a stored, expiring credential (`DOCKERHUB_TOKEN`) that needs manual rotation on some
cadence — which is the original problem that started this whole migration (build broke because
the token expired). A no-expiry Docker Hub access token removes the *expiry* problem but still
means images are public and a long-lived credential sits in GitHub secrets indefinitely.

## Not a real option

Making the Artifact Registry repo public does **not** fix this — the leak is about the project
ID appearing in the image *path string* itself (`.../<project-id>/...`), not about registry
access control. A public AR repo still embeds the project ID in every reference.

## Why not A

Both options were genuine trade-offs — "lose one piece of Flux automation" (A) vs. "keep a
manually-rotated secret and public images" (B) — with no strong technical winner. See
**Decision** above for which was chosen and why.
