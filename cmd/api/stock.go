package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type alphaVantageResponse struct {
	MetaData   map[string]string            `json:"Meta Data"`
	TimeSeries map[string]map[string]string `json:"Time Series (Daily)"`
	Note       string                       `json:"Note,omitempty"`
	Error      string                       `json:"Error Message,omitempty"`
}

type stockResponse struct {
	Symbol        string       `json:"symbol"`
	NDays         int          `json:"ndays"`
	ClosingPrices []priceEntry `json:"closing_prices"`
	Average       float64      `json:"average"`
}

type priceEntry struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

func (app *application) stockHandler(w http.ResponseWriter, r *http.Request) {
	url := fmt.Sprintf(
		"https://www.alphavantage.co/query?apikey=%s&function=TIME_SERIES_DAILY_ADJUSTED&symbol=%s",
		app.config.APIKey,
		app.config.Symbol,
	)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, fmt.Sprintf("upstream error: %v", err))
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, "failed to read upstream response")
		return
	}

	var avResp alphaVantageResponse
	if err := json.Unmarshal(body, &avResp); err != nil {
		app.serverErrorResponse(w, r, http.StatusBadGateway, "failed to parse upstream response")
		return
	}

	if avResp.Error != "" || avResp.Note != "" || len(avResp.TimeSeries) == 0 {
		app.upstreamFailureResponse(w, r, body)
		return
	}

	prices := app.extractClosingPrices(avResp.TimeSeries, app.config.NDays)
	if len(prices) == 0 {
		app.upstreamFailureResponse(w, r, body)
		return
	}

	avg := calculateAverage(prices)

	response := stockResponse{
		Symbol:        app.config.Symbol,
		NDays:         len(prices),
		ClosingPrices: prices,
		Average:       avg,
	}

	err = app.writeJSON(w, http.StatusOK, envelope{"data": response}, nil)
	if err != nil {
		app.serverErrorResponse(w, r, http.StatusInternalServerError, err.Error())
	}
}

func (app *application) extractClosingPrices(timeSeries map[string]map[string]string, ndays int) []priceEntry {
	dates := make([]string, 0, len(timeSeries))
	for date := range timeSeries {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	if len(dates) > ndays {
		dates = dates[:ndays]
	}

	prices := make([]priceEntry, 0, len(dates))
	for _, date := range dates {
		closeStr := timeSeries[date]["4. close"]
		closeVal, err := strconv.ParseFloat(closeStr, 64)
		if err != nil {
			continue
		}
		prices = append(prices, priceEntry{Date: date, Close: closeVal})
	}
	return prices
}

func calculateAverage(prices []priceEntry) float64 {
	if len(prices) == 0 {
		return 0
	}
	var sum float64
	for _, p := range prices {
		sum += p.Close
	}
	return sum / float64(len(prices))
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
