package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/olga-mir/playground-sre/internal/stock"
)

const (
	// EndpointPremium is the type for the premium AlphaVantage endpoint.
	EndpointPremium = "premium"
	// EndpointFree is the type for the free AlphaVantage endpoint.
	EndpointFree = "free"
	// EndpointDemo is the type for the demo AlphaVantage endpoint.
	EndpointDemo = "demo"
	// DemoAPIKey is the API key for the demo endpoint.
	DemoAPIKey = "demo"
	// DemoSymbol is the stock symbol for the demo endpoint.
	DemoSymbol = "IBM"
)

// stockHandler is the handler for the /v1/stock endpoint.
// It fetches stock data from the AlphaVantage API, processes it, and returns it as a JSON response.
// It supports different endpoint types (premium, free, demo) via the "type" query parameter.
func (app *application) stockHandler(w http.ResponseWriter, r *http.Request) {
	endpointType := r.URL.Query().Get("type")
	if endpointType == "" {
		endpointType = EndpointPremium
	}

	// refer to official documentation: https://www.alphavantage.co/documentation/
	url, symbol := app.buildStockURL(endpointType)
	if url == "" {
		app.serverErrorResponse(w, r, http.StatusBadRequest, "invalid endpoint type: use premium, free, or demo")
		return
	}

	avResp, rawPayload, err := app.stockService.Fetch(url)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, err.Error())
		return
	}

	resp, err := app.stockService.Process(avResp, symbol, app.config.NDays)
	if err != nil {
		if errors.Is(err, stock.ErrRateLimited) || errors.Is(err, stock.ErrNoData) {
			app.upstreamFailureResponse(w, r, rawPayload, err, endpointType)
			return
		}
		app.serverErrorResponse(w, r, http.StatusBadGateway, err.Error())
		return
	}

	result := envelope{"data": resp}
	if endpointType != EndpointPremium {
		result["endpoint_type"] = endpointType
	}

	err = app.writeJSON(w, http.StatusOK, result, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}

// buildStockURL constructs the AlphaVantage API URL based on the endpoint type.
// It returns the URL and the stock symbol to be used.
func (app *application) buildStockURL(endpointType string) (url, symbol string) {
	// Symbol can have different formats, e.g.:
	// -- US stocks: `MSFT`, `AAPL`
	// -- London Stock Exchange: `TSCO.LON`
	// -- Toronto Stock Exchange: `SHOP.TRT`
	// -- Shanghai Stock Exchange: `600104.SHH`
	symbol = app.config.Symbol

	switch endpointType {
	case EndpointPremium:
		url = fmt.Sprintf(
			"https://www.alphavantage.co/query?function=TIME_SERIES_DAILY_ADJUSTED&symbol=%s&apikey=%s",
			symbol, app.config.APIKey,
		)
	case EndpointFree:
		url = fmt.Sprintf(
			"https://www.alphavantage.co/query?function=TIME_SERIES_DAILY&symbol=%s&apikey=%s",
			symbol, app.config.APIKey,
		)
	case EndpointDemo:
		symbol = DemoSymbol
		url = fmt.Sprintf(
			"https://www.alphavantage.co/query?function=TIME_SERIES_DAILY_ADJUSTED&symbol=%s&apikey=%s",
			symbol, DemoAPIKey,
		)
	default:
		return "", ""
	}
	return url, symbol
}

// upstreamFailureResponse sends a JSON response for failures from the upstream API.
// It includes the original error, the raw payload from the upstream, and hints for fallback options.
func (app *application) upstreamFailureResponse(w http.ResponseWriter, r *http.Request, originalPayload []byte, err error, endpointType string) {
	var original interface{}
	json.Unmarshal(originalPayload, &original)

	hints := []string{"Try using the /v1/stock-fallback endpoint which uses a static data source"}
	if endpointType == EndpointPremium {
		hints = append(hints, "Try ?type=free for the non-premium endpoint")
		hints = append(hints, "Try ?type=demo for a demo endpoint (fixed symbol: IBM)")
	}

	response := envelope{
		"error":            err.Error(),
		"upstream_payload": original,
		"fallback_hints":   hints,
	}

	app.writeJSON(w, http.StatusBadGateway, response, nil)
}
