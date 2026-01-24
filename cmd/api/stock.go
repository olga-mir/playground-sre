package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"playground-sre/internal/stock"
)

func (app *application) stockHandler(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf(
		"https://www.alphavantage.co/query?apikey=%s&function=TIME_SERIES_DAILY_ADJUSTED&symbol=%s",
		app.config.APIKey,
		app.config.Symbol,
	)

	avResp, rawPayload, err := stock.Fetch(url)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, err.Error())
		return
	}

	resp, failed := stock.Process(avResp, app.config.Symbol, app.config.NDays)
	if failed {
		app.upstreamFailureResponse(w, r, rawPayload)
		return
	}

	err = app.writeJSON(w, http.StatusOK, envelope{"data": resp}, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}

func (app *application) upstreamFailureResponse(w http.ResponseWriter, r *http.Request, originalPayload []byte) {
	var original interface{}
	json.Unmarshal(originalPayload, &original)

	response := envelope{
		"error":            "upstream request failed or returned limited data",
		"upstream_payload": original,
		"fallback_hint":    "Try using the /v1/stock-fallback endpoint which uses a static data source",
	}

	app.writeJSON(w, http.StatusBadGateway, response, nil)
}
