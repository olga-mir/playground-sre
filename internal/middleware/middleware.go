package middleware

import (
	"net/http"

	"golang.org/x/time/rate"
)

type Middleware func(http.Handler) http.Handler

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
