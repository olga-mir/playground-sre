# Open decision: Flux image automation vs. GCP project ID leaking into git

Status: **unresolved, revisit before bringing `apps-dev` back up**

## The problem

`workloads/perf-lab/k8s/deployment.yaml` and `workloads/ebpf-noisy-neighbour/k8s/daemonset.yaml`
currently reference images as:

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

**Current state:** this has not actually happened yet — `apps-dev` isn't running, so
`ImageUpdateAutomation` has never reconciled against these files, and the committed
placeholders are still intact. The risk is latent, not realized. If `apps-dev` comes back up
with the current `ImageRepository`/`ImagePolicy`/`ImageUpdateAutomation` config
(`playground` repo, `kubernetes/tenants/base/sre/` and `sre-ebpf/`) before this is resolved,
the leak **will** occur on the first detected tag change.

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

## Recommendation (non-binding)

No strong recommendation — genuinely a trade-off between "lose one piece of Flux automation"
(A) vs. "keep a manually-rotated secret and public images" (B). Revisit when `apps-dev` is
being brought back up.
