# Stock Ticker API

[![Go](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)](https://github.com/olga-mir/playground-sre/actions/workflows/go.yml/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/olga-mir/playground-sre)](https://goreportcard.com/report/github.com/olga-mir/playground-sre)
[![codecov](https://codecov.io/gh/olga-mir/playground-sre/branch/main/graph/badge.svg)](https://codecov.io/gh/olga-mir/playground-sre)

A RESTful web service that returns closing stock prices from AlphaVantage.

# Quick Start

When running locally following environment variables are expected by the program:

## Configuration

Obtain your key following instructions [here](https://www.alphavantage.co/support/#api-key)

For best experience store env variables in a file and source them before running. All settings are optional.

Note that if API Key is not provided, the main endpoint will return 502, but you can still use fallback mechanisms to interact with this app.

```bash
export APIKEY=<YOUR KEY>
export NDAYS=<NUMBER-OF-DAYS>
export SYMBOL=<TICKER>

# refer to "extras" section for extra config
```

## Run Locally

```bash
go run ./cmd/api

# Test
curl http://localhost:8080/v1/stock
```


## Kubernetes Deployment

```bash
# Create secret (APIKEY from your env)
kubectl create secret generic stock-ticker-secret --from-literal=APIKEY=$APIKEY

# Deploy
kubectl apply -f k8s/
kubectl port-forward svc/stock-ticker 8080:80

# Test (same as local)
curl http://localhost:8080/v1/stock
```

## Taskfile

Install Taskfile: https://taskfile.dev/installation/

If you are on Mac install with `brew`:
```bash
brew install go-task
```

Task is not required to work with this project, equivalents are provided below.
Note that you need to source env variables or provide them in-line, refer to config section at the top of this README.
Also note that docker-push will not work OOTB because my registry is hardcoded in the variable in Taskfile.

<details>
<summary>Bash equivalents (no Taskfile required)</summary>

| Task Command | Bash Equivalent |
|--------------|-----------------|
| `task build` | `go build -o server ./cmd/api` |
| `task run` | `go run ./cmd/api` |
| `task test` | `go test -v ./...` |
| `task docker-build` | `docker build --build-arg GIT_SHA=$(git rev-parse --short HEAD) -t olmigar/stock-ticker:v1 .` |
| `task docker-push` | `docker push olmigar/stock-ticker:v1` |
| `task k8s-apply` | `kubectl apply -f k8s/` |
| `task k8s-delete` | `kubectl delete -f k8s/` |
| `task port-forward` | `kubectl port-forward svc/stock-ticker 8080:80` |

</details>

## Demos and Design Decisions

This project includes a `demo` directory that contains documentation and walkthroughs for various features, showcasing extra-mile efforts in observability and resilience.

For detailed information on design decisions, architecture, and feature demonstrations, please see the [demo README](./demo/README.md).

## Endpoints

- `GET /v1/stock` - Returns last NDAYS closing prices and average for SYMBOL - this relies on premium endpoint, so alternatives are provided:

- `GET /v1/stock-fallback` - Uses static fallback data source (extra)
- `GET /v1/stock?type=demo` - Uses `demo` API Key as documented https://www.alphavantage.co/documentation/
- `GET /v1/stock?type=free` - Uses `TIME_SERIES_DAILY` which is free.

System Endpoints:

- `GET /v1/health` - Health check

## Extras

Optional features configured via `k8s/configmap-extra.yaml`:

| Variable | Description | Default |
|----------|-------------|---------|
| STATIC_FALLBACK_URL | Static data source URL | (empty) |
| ENABLE_CLOUDPROFILER | Enable GCP Cloud Profiler | false |
| GCP_PROJECT_ID | GCP project for profiler | (empty) |

Following tasks have been added in standalone taskfile and intentionally not included in main Taskfile for better focus reviewing Core functionality

```bash
task -t Taskfile.extra.yaml extra:curl-fallback
task -t Taskfile.extra.yaml extra:run-with-fallback
```
