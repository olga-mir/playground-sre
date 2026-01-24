package main

import (
	"net/http"

	"playground-sre/internal/stock"
)

func (app *application) stockFallbackHandler(w http.ResponseWriter, r *http.Request) {
	if app.config.StaticFallbackURL == "" {
		app.serverErrorResponse(w, r, http.StatusServiceUnavailable, "fallback endpoint not configured")
		return
	}

	avResp, _, err := stock.Fetch(app.config.StaticFallbackURL)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "fallback upstream error")
		return
	}

	symbol := app.config.Symbol
	if avResp.MetaData != nil {
		if s, ok := avResp.MetaData["2. Symbol"]; ok {
			symbol = s
		}
	}

	resp, failed := stock.Process(avResp, symbol, app.config.NDays)
	if failed {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "fallback returned no data")
		return
	}

	err = app.writeJSON(w, http.StatusOK, envelope{"data": resp, "source": "fallback"}, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}
