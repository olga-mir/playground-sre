// Package stock provides services for fetching and processing stock data.
package stock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"
)

const (
	// FieldClose is the key for the closing price in the AlphaVantage time series data.
	FieldClose = "4. close"

	// FieldSymbol is the key for the stock symbol in the AlphaVantage metadata.
	FieldSymbol = "2. Symbol"

	// DefaultTimeout is the default timeout for HTTP requests.
	DefaultTimeout = 10 * time.Second

	// MaxNDays is the maximum number of days that can be requested.
	MaxNDays = 365

	// MinNDays is the minimum number of days that can be requested.
	MinNDays = 1
)

var (
	ErrUpstreamError = errors.New("upstream error")
	ErrRateLimited = errors.New("API rate limit exceeded")
	ErrInvalidResponse = errors.New("invalid API response")
	ErrNoData = errors.New("no price data available")
	ErrInvalidNDays = errors.New("ndays must be between 1 and 365")
)

// AlphaVantageResponse represents the structure of the response from the AlphaVantage API.
type AlphaVantageResponse struct {
	MetaData   map[string]string            `json:"Meta Data"`
	TimeSeries map[string]map[string]string `json:"Time Series (Daily)"`
	Note       string                       `json:"Note,omitempty"`
	Error      string                       `json:"Error Message,omitempty"`
}

// Response represents the structure of the response provided by this service.
type Response struct {
	Symbol        string       `json:"symbol"`
	NDays         int          `json:"ndays"`
	ClosingPrices []PriceEntry `json:"closing_prices"`
	Average       float64      `json:"average"`
}

// PriceEntry represents a single day's closing price.
type PriceEntry struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

// Service provides methods for fetching and processing stock data.
type Service struct {
	client *http.Client
}

// NewService creates and returns a new stock Service.
func NewService() *Service {
	return &Service{
		client: &http.Client{Timeout: DefaultTimeout},
	}
}

// Fetch retrieves data from the given URL and attempts to unmarshal it into an AlphaVantageResponse.
// It returns the parsed response, the raw response body, and any error that occurred.
func (s *Service) Fetch(url string) (*AlphaVantageResponse, []byte, error) {
	resp, err := s.client.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUpstreamError, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	var avResp AlphaVantageResponse
	if err := json.Unmarshal(body, &avResp); err != nil {
		return nil, body, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}

	return &avResp, body, nil
}

// Process validates the AlphaVantageResponse, extracts the closing prices, and calculates the average.
// It returns a formatted Response object or an error if processing fails.
func (s *Service) Process(avResp *AlphaVantageResponse, symbol string, ndays int) (*Response, error) {
	if ndays < MinNDays || ndays > MaxNDays {
		return nil, ErrInvalidNDays
	}

	if avResp.Note != "" {
		return nil, fmt.Errorf("%w: %s", ErrRateLimited, avResp.Note)
	}

	if avResp.Error != "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidResponse, avResp.Error)
	}

	if len(avResp.TimeSeries) == 0 {
		return nil, ErrNoData
	}

	prices := extractClosingPrices(avResp.TimeSeries, ndays)
	if len(prices) == 0 {
		return nil, ErrNoData
	}

	return &Response{
		Symbol:        symbol,
		NDays:         len(prices),
		ClosingPrices: prices,
		Average:       calculateAverage(prices),
	}, nil
}

// extractClosingPrices extracts the most recent N days of closing prices from the time series data.
// It sorts the data by date in descending order and returns a slice of PriceEntry.
func extractClosingPrices(timeSeries map[string]map[string]string, ndays int) []PriceEntry {
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
		closeStr := timeSeries[date][FieldClose]
		closeVal, err := strconv.ParseFloat(closeStr, 64)
		if err != nil {
			log.Printf("warning: failed to parse close price for %s: %v", date, err)
			continue
		}
		prices = append(prices, PriceEntry{Date: date, Close: closeVal})
	}
	return prices
}

// calculateAverage calculates the average of the closing prices.
func calculateAverage(prices []PriceEntry) float64 {
	if len(prices) == 0 {
		return 0
	}
	var sum float64
	for _, p := range prices {
		sum += p.Close
	}
	return sum / float64(len(prices))
}
