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
	FieldClose        = "4. close"
	FieldSymbol       = "2. Symbol"
	DefaultTimeout    = 10 * time.Second
	MaxNDays          = 365
	MinNDays          = 1
)

var (
	ErrUpstreamError    = errors.New("upstream error")
	ErrRateLimited      = errors.New("API rate limit exceeded")
	ErrInvalidResponse  = errors.New("invalid API response")
	ErrNoData           = errors.New("no price data available")
	ErrInvalidNDays     = errors.New("ndays must be between 1 and 365")
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

type Service struct {
	client *http.Client
}

func NewService() *Service {
	return &Service{
		client: &http.Client{Timeout: DefaultTimeout},
	}
}

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
