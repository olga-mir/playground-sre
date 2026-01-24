package stock

import (
	"errors"
	"math"
	"testing"
)

func TestExtractClosingPrices(t *testing.T) {
	timeSeries := map[string]map[string]string{
		"2026-01-03": {FieldClose: "150.00"},
		"2026-01-02": {FieldClose: "148.50"},
		"2026-01-01": {FieldClose: "147.00"},
	}

	prices := ExtractClosingPrices(timeSeries, 2)

	if len(prices) != 2 {
		t.Errorf("expected 2 prices, got %d", len(prices))
	}

	if prices[0].Date != "2026-01-03" {
		t.Errorf("expected first date 2026-01-03, got %s", prices[0].Date)
	}

	if prices[0].Close != 150.00 {
		t.Errorf("expected first close 150.00, got %f", prices[0].Close)
	}
}

func TestExtractClosingPricesSkipsInvalid(t *testing.T) {
	timeSeries := map[string]map[string]string{
		"2026-01-02": {FieldClose: "invalid"},
		"2026-01-01": {FieldClose: "100.00"},
	}

	prices := ExtractClosingPrices(timeSeries, 10)

	if len(prices) != 1 {
		t.Errorf("expected 1 valid price, got %d", len(prices))
	}
}

func TestCalculateAverage(t *testing.T) {
	prices := []PriceEntry{
		{Date: "2026-01-03", Close: 100.00},
		{Date: "2026-01-02", Close: 200.00},
		{Date: "2026-01-01", Close: 300.00},
	}

	avg := CalculateAverage(prices)

	if math.Abs(avg-200.00) > 0.001 {
		t.Errorf("expected average 200.00, got %f", avg)
	}
}

func TestCalculateAverageEmpty(t *testing.T) {
	prices := []PriceEntry{}
	avg := CalculateAverage(prices)

	if avg != 0 {
		t.Errorf("expected 0 for empty slice, got %f", avg)
	}
}

func TestServiceProcess(t *testing.T) {
	svc := NewService()
	avResp := &AlphaVantageResponse{
		TimeSeries: map[string]map[string]string{
			"2026-01-02": {FieldClose: "150.00"},
			"2026-01-01": {FieldClose: "100.00"},
		},
	}

	resp, err := svc.Process(avResp, "TEST", 7)

	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}

	if resp.Symbol != "TEST" {
		t.Errorf("expected symbol TEST, got %s", resp.Symbol)
	}

	if resp.NDays != 2 {
		t.Errorf("expected 2 days, got %d", resp.NDays)
	}

	if math.Abs(resp.Average-125.00) > 0.001 {
		t.Errorf("expected average 125.00, got %f", resp.Average)
	}
}

func TestServiceProcessWithError(t *testing.T) {
	svc := NewService()
	avResp := &AlphaVantageResponse{
		Error: "Invalid API call",
	}

	_, err := svc.Process(avResp, "TEST", 7)

	if err == nil {
		t.Error("expected error for invalid response")
	}

	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("expected ErrInvalidResponse, got %v", err)
	}
}

func TestServiceProcessWithNote(t *testing.T) {
	svc := NewService()
	avResp := &AlphaVantageResponse{
		Note: "API call frequency exceeded",
	}

	_, err := svc.Process(avResp, "TEST", 7)

	if err == nil {
		t.Error("expected error for rate limit note")
	}

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestServiceProcessInvalidNDays(t *testing.T) {
	svc := NewService()
	avResp := &AlphaVantageResponse{
		TimeSeries: map[string]map[string]string{
			"2026-01-01": {FieldClose: "100.00"},
		},
	}

	_, err := svc.Process(avResp, "TEST", 0)
	if !errors.Is(err, ErrInvalidNDays) {
		t.Errorf("expected ErrInvalidNDays for 0, got %v", err)
	}

	_, err = svc.Process(avResp, "TEST", 400)
	if !errors.Is(err, ErrInvalidNDays) {
		t.Errorf("expected ErrInvalidNDays for 400, got %v", err)
	}
}

func TestServiceProcessNoData(t *testing.T) {
	svc := NewService()
	avResp := &AlphaVantageResponse{
		TimeSeries: map[string]map[string]string{},
	}

	_, err := svc.Process(avResp, "TEST", 7)

	if !errors.Is(err, ErrNoData) {
		t.Errorf("expected ErrNoData, got %v", err)
	}
}
