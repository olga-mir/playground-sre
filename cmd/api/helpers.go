package main

import (
	"encoding/json"
	"net/http"
)

// envelope is a helper type for wrapping JSON responses.
type envelope map[string]any

// writeJSON marshals data to JSON and writes it to the http.ResponseWriter.
// It also sets the Content-Type header to "application/json".
func (app *application) writeJSON(w http.ResponseWriter, status int, data envelope, headers http.Header) error {
	js, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return err
	}

	js = append(js, '\n')

	for key, value := range headers {
		w.Header()[key] = value
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(js)

	return nil
}