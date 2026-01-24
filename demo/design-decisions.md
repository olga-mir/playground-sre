# Design Decisions

This document outlines the key design decisions made in the Stock Ticker API project.

## API Endpoint Strategy

### Primary Endpoint (Premium)

The core requirement specifies using the AlphaVantage `TIME_SERIES_DAILY_ADJUSTED` endpoint. This is a premium endpoint that:
- Provides adjusted closing prices (accounts for dividends and splits)
- Has stricter rate limits
- Requires a valid API key

**Decision**: Keep this as the default (`/v1/stock`) per the exercise requirements, but provide alternatives when it fails.

### Alternative Endpoints

To handle rate limiting and provide better testing options, we implemented three endpoint types via query parameter:

| Type | Endpoint | API Key | Use Case |
|------|----------|---------|----------|
| `premium` | TIME_SERIES_DAILY_ADJUSTED | User's key | Production (default) |
| `free` | TIME_SERIES_DAILY | User's key | When premium is rate limited |
| `demo` | TIME_SERIES_DAILY_ADJUSTED | `demo` | Testing (fixed symbol: IBM) |

**Usage**: `/v1/stock?type=free` or `/v1/stock?type=demo`

### Static Fallback

For reliable testing without API dependencies, we added a static fallback endpoint:
- Configured via `STATIC_FALLBACK_URL` environment variable
- Returns cached/static data
- No rate limits
- Useful for integration testing and demos

**Endpoint**: `/v1/stock-fallback`

## Symbol Format Support

AlphaVantage supports various ticker formats:
- US stocks: `MSFT`, `AAPL`
- London Stock Exchange: `TSCO.LON`
- Toronto Stock Exchange: `SHOP.TRT`
- Shanghai Stock Exchange: `600104.SHH`

**Decision**: Accept any symbol format and pass through to the API. Validation is delegated to AlphaVantage.

## Error Handling Strategy

### Upstream Failures

When the upstream API fails, we:
1. Relay the original payload back to the user (transparency)
2. Provide actionable fallback hints
3. Return appropriate HTTP status codes

**Response structure**:
```json
{
  "error": "API rate limit exceeded: ...",
  "upstream_payload": { ... },
  "fallback_hints": [
    "Try using the /v1/stock-fallback endpoint",
    "Try ?type=free for the non-premium endpoint",
    "Try ?type=demo for a demo endpoint"
  ]
}
```

### Idiomatic Go Errors

We use sentinel errors for type-safe error handling:
- `ErrRateLimited` - API rate limit exceeded
- `ErrNoData` - No price data available
- `ErrInvalidNDays` - Invalid day range
- `ErrUpstreamError` - Network/connection failures

## Image Tagging

**Decision**: Use stable `v1` tag instead of git SHA for simplicity.

**Rationale**: Git SHA tagging adds CI/CD complexity. For this project's scope and timeline, a stable version tag is sufficient. For production, consider semantic versioning (v1.0.0, v1.1.0, etc.).

## HTTP Client Reuse

**Decision**: Use a shared `stock.Service` with a single `http.Client` instance.

**Rationale**:
- Connection pooling and keep-alive
- Consistent timeout configuration
- Testability via dependency injection
