package surge

import (
	"context"
	"testing"
	"time"
)

type mockDemandReader struct {
	counts map[string]int64
}

func (m *mockDemandReader) IncrementDemand(ctx context.Context, cellHex string, minuteBucket int64, ttl time.Duration) (int64, error) {
	m.counts[cellHex]++
	return m.counts[cellHex], nil
}

func (m *mockDemandReader) GetDemand(ctx context.Context, cellHex string, minuteBucket int64) (int64, error) {
	return m.counts[cellHex], nil
}

func TestComputeMultiplier(t *testing.T) {
	tests := []struct {
		name     string
		demand   int64
		supply   int64
		expected float64
	}{
		{"zero demand", 0, 10, 1.0},
		{"balanced supply demand", 10, 10, 1.0},
		{"surplus supply", 5, 20, 1.0},
		{"moderate surge", 20, 10, 1.35}, // 1.0 + 0.35*(2.0 - 1.0) = 1.35
		{"zero supply high demand", 15, 0, 2.5},
		{"extreme surge capping", 100, 10, 2.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeMultiplier(tt.demand, tt.supply)
			if got != tt.expected {
				t.Errorf("expected multiplier %f, got %f", tt.expected, got)
			}
		})
	}
}

func TestEstimateFarePaisa(t *testing.T) {
	// 5 km ride, 15 minutes, 1.0x surge
	dist := 5000.0
	dur := 15 * time.Minute
	surge := 1.0

	// base 5000 + (5 * 2500 = 12500) + (15 * 300 = 4500) = 22000 paisa (220.00 BDT)
	fare := EstimateFarePaisa(dist, dur, surge)
	if fare != 22000 {
		t.Fatalf("expected 22000 paisa, got %d", fare)
	}

	// With 1.5x surge: 22000 * 1.5 = 33000 paisa
	surgeFare := EstimateFarePaisa(dist, dur, 1.5)
	if surgeFare != 33000 {
		t.Fatalf("expected 33000 paisa, got %d", surgeFare)
	}
}

func TestRecordDemand(t *testing.T) {
	mockStore := &mockDemandReader{counts: make(map[string]int64)}
	calc := NewCalculator(mockStore)

	cellHex, count, err := calc.RecordDemand(context.Background(), 23.7925, 90.4078, time.Now())
	if err != nil {
		t.Fatalf("RecordDemand error: %v", err)
	}
	if cellHex == "" {
		t.Fatal("expected non-empty surge cell hex")
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
}
