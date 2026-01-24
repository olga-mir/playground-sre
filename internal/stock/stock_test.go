package stock

import (
	"math"
	"testing"
)

func TestExtractClosingPrices(t *testing.T) {
	timeSeries := map[string]map[string]string{
		"2026-01-03": {"4. close": "150.00"},
		"2026-01-02": {"4. close": "148.50"},
		"2026-01-01": {"4. close": "147.00"},
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
		"2026-01-02": {"4. close": "invalid"},
		"2026-01-01": {"4. close": "100.00"},
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

func TestProcess(t *testing.T) {
	avResp := &AlphaVantageResponse{
		TimeSeries: map[string]map[string]string{
			"2026-01-02": {"4. close": "150.00"},
			"2026-01-01": {"4. close": "100.00"},
		},
	}

	resp, failed := Process(avResp, "TEST", 7)

	if failed {
		t.Error("expected success, got failure")
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

func TestProcessWithError(t *testing.T) {
	avResp := &AlphaVantageResponse{
		Error: "Invalid API call",
	}

	_, failed := Process(avResp, "TEST", 7)

	if !failed {
		t.Error("expected failure for error response")
	}
}

func TestProcessWithNote(t *testing.T) {
	avResp := &AlphaVantageResponse{
		Note: "API call frequency exceeded",
	}

	_, failed := Process(avResp, "TEST", 7)

	if !failed {
		t.Error("expected failure for rate limit note")
	}
}
