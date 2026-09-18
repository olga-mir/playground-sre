// Package middleware provides HTTP middleware functions.
// Prefer using Chi built-in middlewares first
package middleware

import (
	"net/http"

	"golang.org/x/time/rate"
)

type Middleware func(http.Handler) http.Handler

// RateLimiter returns a middleware that limits the number of requests per second.
// If the limit is exceeded, the onLimit function is called.
func RateLimiter(rps float64, burst int, onLimit func(http.ResponseWriter, *http.Request)) func(http.Handler) http.Handler {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow() {
				onLimit(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
