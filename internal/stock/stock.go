package stock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type AlphaVantageResponse struct {
	MetaData   map[string]string            `json:"Meta Data"`
	TimeSeries map[string]map[string]string `json:"Time Series (Daily)"`
	Note       string                       `json:"Note,omitempty"`
	Error      string                       `json:"Error Message,omitempty"`
}

type Response struct {
	Symbol        string       `json:"symbol"`
	NDays         int          `json:"ndays"`
	ClosingPrices []PriceEntry `json:"closing_prices"`
	Average       float64      `json:"average"`
}

type PriceEntry struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

type FetchResult struct {
	Response    *Response
	RawPayload  []byte
	UpstreamErr bool
}

func Fetch(url string) (*AlphaVantageResponse, []byte, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("upstream error: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	var avResp AlphaVantageResponse
	if err := json.Unmarshal(body, &avResp); err != nil {
		return nil, body, fmt.Errorf("failed to parse response: %w", err)
	}

	return &avResp, body, nil
}

func Process(avResp *AlphaVantageResponse, symbol string, ndays int) (*Response, bool) {
	if avResp.Error != "" || avResp.Note != "" || len(avResp.TimeSeries) == 0 {
		return nil, true
	}

	prices := ExtractClosingPrices(avResp.TimeSeries, ndays)
	if len(prices) == 0 {
		return nil, true
	}

	return &Response{
		Symbol:        symbol,
		NDays:         len(prices),
		ClosingPrices: prices,
		Average:       CalculateAverage(prices),
	}, false
}

func ExtractClosingPrices(timeSeries map[string]map[string]string, ndays int) []PriceEntry {
	dates := make([]string, 0, len(timeSeries))
	for date := range timeSeries {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	if len(dates) > ndays {
		dates = dates[:ndays]
	}

	prices := make([]PriceEntry, 0, len(dates))
	for _, date := range dates {
		closeStr := timeSeries[date]["4. close"]
		closeVal, err := strconv.ParseFloat(closeStr, 64)
		if err != nil {
			continue
		}
		prices = append(prices, PriceEntry{Date: date, Close: closeVal})
	}
	return prices
}

func CalculateAverage(prices []PriceEntry) float64 {
	if len(prices) == 0 {
		return 0
	}
	var sum float64
	for _, p := range prices {
		sum += p.Close
	}
	return sum / float64(len(prices))
}
