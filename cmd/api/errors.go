package main

import (
	"log"
	"net/http"
)

// logError logs an error with the request method and URI.
func (app *application) logError(r *http.Request, err error) {
	log.Printf("error: %s %s: %v", r.Method, r.URL.Path, err)
}

// serverErrorResponse sends a JSON-formatted error message to the client.
func (app *application) serverErrorResponse(w http.ResponseWriter, r *http.Request, status int, message interface{}) {
	env := envelope{"error": message}
	err := app.writeJSON(w, status, env, nil)
	if err != nil {
		app.logError(r, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// notFoundResponse sends a 404 Not Found response to the client.
func (app *application) notFoundResponse(w http.ResponseWriter, r *http.Request) {
	app.serverErrorResponse(w, r, http.StatusNotFound, "the requested resource could not be found")
}

// rateLimitExceededResponse sends a 429 Too Many Requests response to the client.
func (app *application) rateLimitExceededResponse(w http.ResponseWriter, r *http.Request) {
	app.serverErrorResponse(w, r, http.StatusTooManyRequests, "rate limit exceeded")
}
