# Goal

Implement a simple web restful api web server in go. We'll do it in two stages: core stage - implementation that satisfies all requirements and extras - additional features on best effort basis, with particular focus on application reliability.

## Core Requirements Part

The core requirements can be found in `../aux/requirements-description.md` file.
The endpoint described in the exercise might be rate limited, or not available, and we are not going to use it until very final testing stage. But it should be implemented in initial stage. (think TDD)

This endpoint is tricky and it is now premium endpoint. We need to call the endpoint as described in the requirements, when it fails the original payload from the upstream must be relayed back to user, but we will also offer our instructions on the next best option - which is to call a downgraded version of this endpoint. We don't need to implement this particular fallback, just inform user so that they have meaningful information and a way forward.

The payload sample for the original endpoint is stored in `sample-payload.json`

Implemement bare minimum for core stage. Try to be concise, no fancy tests, no comments, no extra features, parameters or inputs.

## Extras

With Extras it is important that they are as stand alone as possible and do not interfere in any noticeable way with core functionality.
They need to be clearly separated out so that code for core requirements can be easily understood. For example when adding tasks that are not required for Core, add them in "extra:" prefix and store them in a separate Taskfile. Core tasks should be in main Taskfile.

I have an endpoint which is not ratelimited, does not require any keys or auth and returns the same content as the original one, we will use it for testing and expose it on a different endpoint on our app. We don't have staging env, so it needs to live in the same binary.

```
https://head-in-the-cloudz.com/experiments/73/static-fallback
```

Do not hardcode this endpoint but mount additional ConfigMap with extra parameters. from k8s perspective we will have to mount both configmaps to pods, but webserver should not rely on "extra" params - they need to be optional with sensible defaults.


Additional parameters that might be needed:
* `STATIC_FALLBACK_URL`
* `GCP_PROJECT_ID` optional, default `""` and it is not used
* `ENABLE_CLOUDPROFILER` optional, default false

`GCP_PROJECT_ID` should not be committed to the repo, but you MUST use it from the env variables in the terminal - in this project we do rely on env variables.

If cloud profiler is enabled and project is provided our app need to enable profiling (technically `GCP_PROJECT_ID` is not needed when it runs in the cloud, but this condition will keep it simple)

This is snippet for cloud profiler taken from Google page. In our case we need to start it conditionally.

```
package main

import (
	"cloud.google.com/go/profiler"
)

func main() {
	cfg := profiler.Config{
		Service:        "myservice",
		ServiceVersion: "1.0.0",
		// ProjectID must be set if not running on GCP.
		// ProjectID: "my-project",
	}

	// Profiler initialization, best done as early as possible.
	if err := profiler.Start(cfg); err != nil {
		// TODO: Handle error.
	}
}
```

# Tools and Environment

## Cluster

Assume a kind or a minikube cluster exists. It is a vanilla k8s cluster, without service mesh or any fancy features
IMPORTANT: In this project we need to work with current context in the terminal where this project is run, assume it is safe.

## Taskfile

Taskfile - we will use Taskfile in this project, mention in the README with quick instructions how to install it. However user should be able to work with this project without extra tools.
For this purpose, can you please add a section in the README, under expand (details html tag) a simple table that translates task commands to bash commands, only absolute minimum which is required for the Core Requirements

## Image

Build stage: golang:1.25-bookworm
Run stage: gcr.io/distroless/base-debian12

This is for potential extension to use Cloud Profiler, which might not work with Alpine.

# Project Structure

I have a foundational working webserver skeleton here: ${HOME}/repos/experiments/73-2026.01-rust-go-webserver/go

Take all golang folders and files as is, except leaving their business logic and handlers out e.g. `go/cmd/api/cpu.go`, `go/cmd/api/io.go`.
Take Dockerfile but update to the image requiments outlined here.
Take Taskfile but strip out registry, and anything else which is not relevant here.
Take a look at README - it has some similar goals and ideas for future reliability features.

