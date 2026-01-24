package main

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

func (app *application) stockFallbackHandler(w http.ResponseWriter, r *http.Request) {
	if app.config.StaticFallbackURL == "" {
		app.serverErrorResponse(w, r, http.StatusServiceUnavailable, "fallback endpoint not configured")
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(app.config.StaticFallbackURL)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "fallback upstream error")
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, "failed to read fallback response")
		return
	}

	var avResp alphaVantageResponse
	if err := json.Unmarshal(body, &avResp); err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "failed to parse fallback response")
		return
	}

	if len(avResp.TimeSeries) == 0 {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "fallback returned no data")
		return
	}

	prices := app.extractClosingPrices(avResp.TimeSeries, app.config.NDays)
	avg := calculateAverage(prices)

	symbol := app.config.Symbol
	if avResp.MetaData != nil {
		if s, ok := avResp.MetaData["2. Symbol"]; ok {
			symbol = s
		}
	}

	response := stockResponse{
		Symbol:        symbol,
		NDays:         len(prices),
		ClosingPrices: prices,
		Average:       avg,
	}

	err = app.writeJSON(w, http.StatusOK, envelope{"data": response, "source": "fallback"}, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}
