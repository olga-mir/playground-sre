# Stock Ticker API

A RESTful web service that returns closing stock prices from AlphaVantage.

# Quick Start

When running locally following environment variables are expected by the program:

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| SYMBOL | Stock symbol to query | MSFT |
| NDAYS | Number of days to return | 7 |
| APIKEY | AlphaVantage API key | (required) |
| SERVER_ADDRESS | Listen address | :8080 |

Obtain your key following instructions [here](https://www.alphavantage.co/support/#api-key)

## Run Locally

```bash
go run ./cmd/api
# OR pass env vars explicitely
APIKEY=<YOUKEY> NDAYS=5 SYMBOL=MSFT go run ./cmd/api

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
```

## Taskfile

Install Taskfile: https://taskfile.dev/installation/

If you are on Mac install with `brew`:
```bash
brew install go-task
```

<details>
<summary>Bash equivalents (no Taskfile required)</summary>

| Task Command | Bash Equivalent |
|--------------|-----------------|
| `task build` | `go build -o server ./cmd/api` |
| `task run` | `go run ./cmd/api` |
| `task test` | `go test -v ./...` |
| `task docker-build` | `docker build --build-arg GIT_SHA=$(git rev-parse --short HEAD) -t olmigar/stock-ticker:latest .` |
| `task docker-push` | `docker push olmigar/stock-ticker:latest` |
| `task k8s-apply` | `kubectl apply -f k8s/` |
| `task k8s-delete` | `kubectl delete -f k8s/` |
| `task port-forward` | `kubectl port-forward svc/stock-ticker 8080:80` |

</details>

## Endpoints


- `GET /v1/stock` - Returns last NDAYS closing prices and average for SYMBOL

Additional Endpoints:

- `GET /v1/health` - Health check
- `GET /v1/stock-fallback` - Uses static fallback data source (extra)

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
