package main

import (
	"net/http"

	"github.com/olga-mir/playground-sre/internal/stock"
)

// stockFallbackHandler is the handler for the /v1/stock-fallback endpoint.
// It fetches stock data from a static, predefined URL, processes it, and returns it as a JSON response.
// This handler is intended to be used as a fallback when the primary stock endpoint fails.
func (app *application) stockFallbackHandler(w http.ResponseWriter, r *http.Request) {
	if app.config.StaticFallbackURL == "" {
		app.serverErrorResponse(w, r, http.StatusServiceUnavailable, "fallback endpoint not configured")
		return
	}

	avResp, _, err := app.stockService.Fetch(app.config.StaticFallbackURL)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "fallback upstream error")
		return
	}

	symbol := app.config.Symbol
	if avResp.MetaData != nil {
		if s, ok := avResp.MetaData[stock.FieldSymbol]; ok {
			symbol = s
		}
	}

	resp, err := app.stockService.Process(avResp, symbol, app.config.NDays)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, err.Error())
		return
	}

	err = app.writeJSON(w, http.StatusOK, envelope{"data": resp, "source": "fallback"}, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}
