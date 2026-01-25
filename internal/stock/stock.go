// Package stock provides services for fetching and processing stock data.
package stock

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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
	// ErrUpstreamError indicates an error from the upstream API.
	ErrUpstreamError = errors.New("upstream error")
	// ErrRateLimited indicates that the API rate limit has been exceeded.
	ErrRateLimited = errors.New("API rate limit exceeded")
	// ErrInvalidResponse indicates an invalid or unparseable response from the API.
	ErrInvalidResponse = errors.New("invalid API response")
	// ErrNoData indicates that no price data is available for the requested symbol or date range.
	ErrNoData = errors.New("no price data available")
	// ErrInvalidNDays indicates that the requested number of days is outside the valid range.
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
func (s *Service) Fetch(rawURL string) (*AlphaVantageResponse, []byte, error) {
	// Redact API key from URL for logging
	logURL, err := redactAPIKey(rawURL)
	if err != nil {
		log.Printf("warning: failed to parse URL for logging: %v", err)
		logURL = "malformed-url"
	}
	log.Printf("info: calling upstream endpoint: %s", logURL)

	resp, err := s.client.Get(rawURL)
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

	prices := ExtractClosingPrices(avResp.TimeSeries, ndays)
	if len(prices) == 0 {
		return nil, ErrNoData
	}

	return &Response{
		Symbol:        symbol,
		NDays:         len(prices),
		ClosingPrices: prices,
		Average:       CalculateAverage(prices),
	}, nil
}

// ExtractClosingPrices extracts the most recent N days of closing prices from the time series data.
// It sorts the data by date in descending order and returns a slice of PriceEntry.
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

// CalculateAverage calculates the average of the closing prices.
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

// redactAPIKey takes a raw URL string and returns a version with the "apikey" query parameter redacted.
func redactAPIKey(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := parsedURL.Query()
	if q.Has("apikey") {
		q.Set("apikey", "***REDACTED***")
	}
	parsedURL.RawQuery = q.Encode()
	return parsedURL.String(), nil
}
