package main

import (
	"log"
	"net/http"
)

func (app *application) logError(r *http.Request, err error) {
	log.Printf("error: %s %s: %v", r.Method, r.URL.Path, err)
}

func (app *application) serverErrorResponse(w http.ResponseWriter, r *http.Request, status int, message interface{}) {
	env := envelope{"error": message}
	err := app.writeJSON(w, status, env, nil)
	if err != nil {
		app.logError(r, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (app *application) notFoundResponse(w http.ResponseWriter, r *http.Request) {
	app.serverErrorResponse(w, r, http.StatusNotFound, "the requested resource could not be found")
}

func (app *application) rateLimitExceededResponse(w http.ResponseWriter, r *http.Request) {
	app.serverErrorResponse(w, r, http.StatusTooManyRequests, "rate limit exceeded")
}
