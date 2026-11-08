# NN — Title phrased as a question or a symptom

> **Time:** ~N min · **Needs:** local only | GKE cluster | GKE + node pool · **Cost:** none | ~$X/hr while the cluster is up
>
> **Prerequisites:** [00 — Prerequisites](../00-prerequisites.md) (section X), tutorials NN, NN

## The question

One or two sentences: the symptom a reader might see in production, phrased the way they would
phrase it. ("p99 doubled but CPU looks fine.")

## Hypothesis

What you expect to happen and why, stated before running anything. Readers learn more from
checking a prediction than from reading a result.

## Setup

Exact commands, nothing implicit. Prefer `task` targets over raw `kubectl`.

```bash
task ...
```

## Run

The experiment itself. Keep each step to one command plus one sentence of what it does.

## What to look at

The query, dashboard panel or tool output that answers the question. Include the PromQL, flame
graph filter or bpftrace one-liner verbatim.

## What you should see

Expected output or screenshot (committed under `results/` so readers without a cluster can follow
along). Say which numbers matter and which are environment-dependent.

## Why

The mechanism. This is the part that transfers to other systems. Link to research notes or
upstream docs rather than inlining long derivations.

## Clean up

```bash
task ...
```

## Tested on

Kernel / GKE version / machine type / date. Keep dated, cluster-specific detail here rather than
in the body.

## Next

Link to the tutorial that builds on this one.
