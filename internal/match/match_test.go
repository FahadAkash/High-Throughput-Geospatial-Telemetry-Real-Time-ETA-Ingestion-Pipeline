package match

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/routing"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/postgres"
	redisStore "github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/redis"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

type inMemRedisStore struct {
	mu      sync.Mutex
	drivers map[string]*redisStore.DriverState
	claims  map[string]string // driverID -> rideID
}

func newInMemRedisStore() *inMemRedisStore {
	return &inMemRedisStore{
		drivers: make(map[string]*redisStore.DriverState),
		claims:  make(map[string]string),
	}
}

func (s *inMemRedisStore) GetActiveDriversInCells(ctx context.Context, cellHexes []string, minValidTs int64) ([]redisStore.CandidateScore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []redisStore.CandidateScore
	for _, d := range s.drivers {
		if d.Ts >= minValidTs {
			for _, hex := range cellHexes {
				if d.Cell == hex {
					result = append(result, redisStore.CandidateScore{DriverID: d.DriverID, TsMs: d.Ts})
					break
				}
			}
		}
	}
	return result, nil
}

func (s *inMemRedisStore) GetDriverStates(ctx context.Context, driverIDs []string) (map[string]*redisStore.DriverState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res := make(map[string]*redisStore.DriverState)
	for _, id := range driverIDs {
		if d, ok := s.drivers[id]; ok {
			res[id] = d
		}
	}
	return res, nil
}

func (s *inMemRedisStore) ClaimDriver(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.claims[driverID]; exists {
		return false, nil // Conflict!
	}
	s.claims[driverID] = rideID
	return true, nil
}

func (s *inMemRedisStore) ReleaseClaim(ctx context.Context, driverID, rideID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if holder, exists := s.claims[driverID]; exists && holder == rideID {
		delete(s.claims, driverID)
		return true, nil
	}
	return false, nil
}

type mockRouter struct{}

func (m *mockRouter) ComputeTable(
	ctx context.Context,
	riderLat, riderLng float64,
	riderCell string,
	candidates []routing.Candidate,
	now time.Time,
) ([]routing.CandidateETA, error) {
	res := make([]routing.CandidateETA, len(candidates))
	for i, c := range candidates {
		// Calculate simulated ETA: 1 second per 6 meters
		dur := time.Duration(c.DistM/6.0) * time.Second
		res[i] = routing.CandidateETA{
			Candidate: c,
			DistanceM: c.DistM,
			Duration:  dur,
		}
	}
	return res, nil
}

type mockRepo struct {
	mu    sync.Mutex
	rides []*postgres.Ride
}

func (m *mockRepo) CreateRide(ctx context.Context, ride *postgres.Ride) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rides = append(m.rides, ride)
	return nil
}

func TestMatchSuccessful(t *testing.T) {
	now := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	memStore := newInMemRedisStore()
	repo := &mockRepo{}
	router := &mockRouter{}

	riderLat := 23.7925
	riderLng := 90.4078
	riderCell, _ := geo.LatLngToCell(riderLat, riderLng, geo.DispatchResolution)
	riderCellHex := geo.CellToHex(riderCell)

	// Add 2 active drivers:
	// Driver 1: Very close (50m)
	memStore.drivers["drv_close"] = &redisStore.DriverState{
		DriverID: "drv_close",
		Lat:      23.7927,
		Lng:      90.4080,
		Cell:     riderCellHex,
		Ts:       now.UnixMilli(),
		Status:   "available",
	}

	// Driver 2: Further away (1.5km)
	memStore.drivers["drv_far"] = &redisStore.DriverState{
		DriverID: "drv_far",
		Lat:      23.8050,
		Lng:      90.4150,
		Cell:     riderCellHex,
		Ts:       now.UnixMilli(),
		Status:   "available",
	}

	svc := NewService(memStore, memStore, router, nil, repo, clock, 15*time.Second, 10*time.Second, 10)

	req := &MatchRequest{
		RideID:     "ride_001",
		RiderID:    "rider_abc",
		PickupLat:  riderLat,
		PickupLng:  riderLng,
		DropoffLat: 23.7330,
		DropoffLng: 90.4172,
	}

	resp, err := svc.Match(context.Background(), req)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}

	if resp.DriverID != "drv_close" {
		t.Fatalf("expected closest driver drv_close, got %s", resp.DriverID)
	}

	if len(repo.rides) != 1 || repo.rides[0].RideID != "ride_001" {
		t.Fatalf("ride was not saved to repository: %+v", repo.rides)
	}
}

func TestMatchClaimContention(t *testing.T) {
	now := time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	memStore := newInMemRedisStore()
	repo := &mockRepo{}
	router := &mockRouter{}

	riderLat := 23.7925
	riderLng := 90.4078
	riderCell, _ := geo.LatLngToCell(riderLat, riderLng, geo.DispatchResolution)
	riderCellHex := geo.CellToHex(riderCell)

	// 1 driver available
	memStore.drivers["drv_single"] = &redisStore.DriverState{
		DriverID: "drv_single",
		Lat:      23.7926,
		Lng:      90.4079,
		Cell:     riderCellHex,
		Ts:       now.UnixMilli(),
		Status:   "available",
	}

	svc := NewService(memStore, memStore, router, nil, repo, clock, 15*time.Second, 10*time.Second, 10)

	// Two simultaneous riders request at the exact same time
	var wg sync.WaitGroup
	wg.Add(2)

	var winnerCount int
	var mu sync.Mutex

	for i := 1; i <= 2; i++ {
		rideID := fmt.Sprintf("ride_contend_%d", i)
		go func(rID string) {
			defer wg.Done()
			req := &MatchRequest{
				RideID:     rID,
				RiderID:    "rider_x",
				PickupLat:  riderLat,
				PickupLng:  riderLng,
				DropoffLat: 23.7330,
				DropoffLng: 90.4172,
			}
			resp, err := svc.Match(context.Background(), req)
			if err == nil && resp != nil {
				mu.Lock()
				winnerCount++
				mu.Unlock()
			}
		}(rideID)
	}

	wg.Wait()

	// Exactly 1 rider must have matched the single driver; the other must fail safely
	if winnerCount != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", winnerCount)
	}
}
