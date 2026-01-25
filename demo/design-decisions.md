# Design Decisions

This document outlines the key design decisions made in the Stock Ticker API project.

## API Endpoint Strategy

The core requirement specifies using the AlphaVantage `TIME_SERIES_DAILY_ADJUSTED` endpoint. This is a premium endpoint that requires premium subscription valid API key and has rate limits

**Decision**: Keep this as the default (`/v1/stock`) per the exercise requirements, but provide alternatives when it fails. This approach strikes balance between business requirements, user experience and testability during deployment.

### Alternative Endpoints

To handle rate limiting and provide better testing options, we implemented three endpoint types via query parameter:

| Type | Endpoint | API Key | Use Case |
|------|----------|---------|----------|
| `premium` | `TIME_SERIES_DAILY_ADJUSTED` | User's key | Production (default) |
| `free` | `TIME_SERIES_DAILY` | User's key | When premium is rate limited |
| `demo` | `TIME_SERIES_DAILY_ADJUSTED` | `demo` | Testing (fixed symbol: IBM) |

**Usage**: `/v1/stock?type=free` or `/v1/stock?type=demo`

### Static Fallback

For reliable testing without API dependencies, we added a static fallback endpoint. Instead of external vendor it "queries" an endpoint on public internet which is hosted on infrastructure we manage and sends the same payload (but static). It is available at https://head-in-the-cloudz.com/experiments/73/static-fallback

User Experience:

```bash
% curl localhost:8080/v1/stock
{
        "error": "no price data available",
        "fallback_hints": [
                "Try using the /v1/stock-fallback endpoint which uses a static data source",
                "Try ?type=free for the non-premium endpoint",
                "Try ?type=demo for a demo endpoint (fixed symbol: IBM)"
        ],
        "upstream_payload": {
                "Information": "Thank you for using Alpha Vantage! This is a premium endpoint. You may subscribe to any of the premium plans at https://www.alphavantage.co/premium/ to instantly unlock all premium endpoints"
        }
}
% curl "localhost:8080/v1/stock?type=demo"
{
        "data": {
                "symbol": "IBM",
                "ndays": 5,
                "closing_prices": [
                        {
                                "date": "2026-01-23",
                                "close": 292.44
                        },
                        ....
                ],
                "average": 296.334
        },
        "endpoint_type": "demo"
}
```

## Go Architecture

* Clean idiomatic Go folder structure - routes and APIs in `cmd/api`, business logic in `internal`
* Chi router - lightweight and idiomatic—built on stdlib modern Go router widely used in production. Cloudflare uses it in cloudflared `management` APIs, but it is definitely not the only example. Chi middleware composability is excellent and easy to use.
* Use of envelope and common errors ensures consistent user experience, as well as developer experience - by managing it in one central place, developers can use this as a pattern throughout the code. This approach is heavily inspired by https://lets-go-further.alexedwards.net/sample/03.06-sending-error-messages.html
* Factory pattern and closures to allow encapsulation and avoid global variables.
* Testing - this project did not follow TDD due to its nature and initial state that was available to me. Tests were generated alongside code, and should have used Table tests pattern.

## Other Aspects

* no CPU limit. Devisive topic with lots been said on the both sides. My stance is add this only when determinism is required - PnV (Performance and Volume testing). Set CPU requests that make sence - if the app consistently relies on spare cycles, it is asking for trouble and is a time bomb.
* `imagePullPolicy: Always` - this may feel as anti-resilience pattern, but I'd argue security benefit outweigh potential delays in image pull. Note that the full image payload is not pulled if image exists on the node, however registry credentials are verified when `Always` is used. Yes it does include a network call to registry and it is a dependency, however additional Supply Chain control is good to have. When registry is unavailable for whatever reason, or network path unreliable - this is likely you are already in an incident. New nodes won't be able to start and you have a bigger fish to prey. This network call can affect rollout speed, which in my view is also not a major operational concern.
* HPA / VPA - not relevant in "vanilla" cluster if metrics server is not available.
