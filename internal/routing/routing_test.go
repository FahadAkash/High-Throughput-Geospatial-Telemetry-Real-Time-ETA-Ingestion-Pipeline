package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type mockETACacheReader struct {
	store map[string]int64
}

func (m *mockETACacheReader) GetCachedETA(ctx context.Context, origHex, destHex string, bucket5m int64) (int64, bool, error) {
	key := origHex + ":" + destHex
	val, ok := m.store[key]
	return val, ok, nil
}

func (m *mockETACacheReader) SetCachedETA(ctx context.Context, origHex, destHex string, bucket5m int64, durationSec int64, ttl time.Duration) error {
	key := origHex + ":" + destHex
	m.store[key] = durationSec
	return nil
}

func TestETACacheHitAndMiss(t *testing.T) {
	mockStore := &mockETACacheReader{store: make(map[string]int64)}
	cache := NewETACache(mockStore, 5*time.Minute)

	orig := "8865616521fffff"
	dest := "8865616523fffff"
	now := time.Now()

	// 1. Initial lookup -> Miss
	_, hit := cache.Get(context.Background(), orig, dest, now)
	if hit {
		t.Fatal("expected cache miss, got hit")
	}

	// 2. Set cache
	cache.Set(context.Background(), orig, dest, now, 180*time.Second)

	// 3. Second lookup -> Hit
	duration, hit := cache.Get(context.Background(), orig, dest, now)
	if !hit {
		t.Fatal("expected cache hit, got miss")
	}
	if duration != 180*time.Second {
		t.Fatalf("expected 180s, got %v", duration)
	}
}

func TestOSRMClientTableWithMockServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": "Ok",
			"durations": [[150.0], [240.0]],
			"distances": [[1000.0], [1600.0]]
		}`))
	}))
	defer ts.Close()

	client := NewOSRMClient(ts.URL, nil)

	candidates := []Candidate{
		{DriverID: "d1", Lat: 23.7925, Lng: 90.4078, CellHex: "c1", DistM: 800},
		{DriverID: "d2", Lat: 23.7930, Lng: 90.4085, CellHex: "c2", DistM: 1200},
	}

	results, err := client.ComputeTable(context.Background(), 23.7910, 90.4060, "riderCell", candidates, time.Now())
	if err != nil {
		t.Fatalf("ComputeTable error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Duration != 150*time.Second {
		t.Errorf("expected 150s for d1, got %v", results[0].Duration)
	}
	if results[1].Duration != 240*time.Second {
		t.Errorf("expected 240s for d2, got %v", results[1].Duration)
	}
}

func TestOSRMClientFallback(t *testing.T) {
	// Points to unreachable URL to trigger fallback heuristic
	client := NewOSRMClient("http://127.0.0.1:1", nil)

	candidates := []Candidate{
		{DriverID: "d1", Lat: 23.7925, Lng: 90.4078, CellHex: "c1", DistM: 500},
	}

	results, err := client.ComputeTable(context.Background(), 23.7910, 90.4060, "riderCell", candidates, time.Now())
	if err != nil {
		t.Fatalf("expected fallback not error, got %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Duration <= 0 {
		t.Errorf("expected positive duration from fallback heuristic, got %v", results[0].Duration)
	}
}
