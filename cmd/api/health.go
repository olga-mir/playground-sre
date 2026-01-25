package main

import (
	"net/http"
)

// healthHandler is the handler for the /v1/health endpoint.
// It returns a JSON response with the status of the service and the current version.
func (app *application) healthHandler(w http.ResponseWriter, r *http.Request) {
	response := envelope{
		"status":  "healthy",
		"version": gitSHA,
	}

	err := app.writeJSON(w, http.StatusOK, response, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}
