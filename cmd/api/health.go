package main

import (
	"net/http"
)

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
