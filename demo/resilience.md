# Resilience Features

This document outlines the resilience and reliability features implemented in the Stock Ticker API.

## Rate Limiting

### Client-Side Rate Limiting

We implement per-endpoint rate limiting using `golang.org/x/time/rate`:

| Endpoint | Requests/Second | Burst |
|----------|-----------------|-------|
| `/v1/stock` | 1 | 1 |
| `/v1/stock-fallback` | 20 | 5 |
| `/v1/health` | No limit | - |

**Rationale**:
- Stock endpoint is aggressive (1 rps) because AlphaVantage has API rate limits. Current implementation does not give us acceptable guarantees, but it shows intent.
- Fallback is more permissive since it hits a static endpoint, which is hosted on infrastructure I manage

### Handling Upstream Rate Limits

When AlphaVantage returns a rate limit error:
- Detect `Note` field in the response
- Suggest alternative endpoints in the response - better user-experience

Examples from AlphaVantage responses advising on rate limits
```
  "Note": "Thank you for using Alpha Vantage! Our standard API rate limit is 25 requests per day. Please subscribe to any of the premium plans at https://www.alphavantage.co/premium/ to instantly remove all daily rate limits."
  "Note": "Thank you for using Alpha Vantage! Our standard API call frequency is 5 calls per minute and 500 calls per day. Please visit https://www.alphavantage.co/premium/ if you would like to target a higher API call frequency."
```

## Graceful Shutdown

The server handles SIGINT and SIGTERM signals:

```go
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
server.Shutdown(ctx)
```

**Benefits**:
- In-flight requests complete before shutdown
- 10-second timeout prevents hanging
- Clean container termination in Kubernetes, which is important in scale down and node events.

## Kubernetes Probes

Liveness and readiness probes to avoid routing requests when the pod is experiencing problems and may not be able to properly server the requests

```yaml
livenessProbe:
  httpGet:
    path: /v1/health
    port: http
  initialDelaySeconds: 5
  periodSeconds: 10
```

## Timeouts

Used in both client and server, context passed to handlers, idiomatic in Go.

## Error and Panic Recovery

Chi's `middleware.Recoverer` catches panics and returns 500 errors instead of crashing.

## Fallback Strategy

When the primary endpoint fails, users have multiple fallback options.
Implements the business requirements, but provides better user experience providing alternatives and instructions.

```
/v1/stock (premium)
    ↓ fails
/v1/stock?type=free
    ↓ fails
/v1/stock?type=demo
    ↓ not suitable
/v1/stock-fallback (static)
```

## Observability

When enabled (`ENABLE_CLOUDPROFILER=true`), CPU and heap profiling data is sent to GCP.
Logging as middleware.

Future improvements include - prom metrics, OTEL instrumentation.

## Future Improvements

A lot more can be implemented but I am an advocate for a holistic approach. First and foremost understanding the environment it is running in:
- Service Mesh: resilience is one of the core pillars of most modern service mesh implementations
- Managed k8s Runtime: Cloud Providers offer comformant but extremely highly-opinionated k8s distributions, they are also very tightly integrated into wider ecosystem offerring many features OOTB including in o11y and reliability
- Higher level managed platform: e.g. Serverless like Cloud Run or Fargate.

Adopted frameworks and internal libraries are also important factor to take into consideration.
