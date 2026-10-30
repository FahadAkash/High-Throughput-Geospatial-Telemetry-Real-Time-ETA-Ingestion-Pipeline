package match

import (
	"context"
	"fmt"
	"time"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/routing"
	redisStore "github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/redis"
)

// CandidateReader defines interface to fetch candidates from Redis.
type CandidateReader interface {
	GetActiveDriversInCells(ctx context.Context, cellHexes []string, minValidTs int64) ([]redisStore.CandidateScore, error)
	GetDriverStates(ctx context.Context, driverIDs []string) (map[string]*redisStore.DriverState, error)
}

// FindCandidates executes a pipelined K-Ring candidate search around the rider's location.
// Invariant: Always reads rider cell plus k-ring neighbors (k=1, 7 cells) in one round trip.
func FindCandidates(
	ctx context.Context,
	reader CandidateReader,
	riderLat, riderLng float64,
	staleWindow time.Duration,
	now time.Time,
) (string, []routing.Candidate, error) {
	// 1. Rider Cell & K-Ring
	riderCell, err := geo.LatLngToCell(riderLat, riderLng, geo.DispatchResolution)
	if err != nil {
		return "", nil, fmt.Errorf("getting rider cell: %w", err)
	}
	riderCellHex := geo.CellToHex(riderCell)

	kRingCells, err := geo.KRing(riderCell, geo.DefaultKRing)
	if err != nil {
		return "", nil, fmt.Errorf("calculating k-ring for cell %s: %w", riderCellHex, err)
	}

	cellHexes := make([]string, len(kRingCells))
	for i, c := range kRingCells {
		cellHexes[i] = geo.CellToHex(c)
	}

	// 2. Fetch all drivers with timestamp >= minValidTs across all 7 cells in 1 pipelined round-trip
	minValidTs := now.Add(-staleWindow).UnixMilli()
	candScores, err := reader.GetActiveDriversInCells(ctx, cellHexes, minValidTs)
	if err != nil {
		return riderCellHex, nil, fmt.Errorf("querying active drivers in k-ring cells: %w", err)
	}

	if len(candScores) == 0 {
		return riderCellHex, nil, nil
	}

	driverIDs := make([]string, len(candScores))
	for i, s := range candScores {
		driverIDs[i] = s.DriverID
	}

	// 3. Batch fetch coordinates and status from drv:{id} hashes
	states, err := reader.GetDriverStates(ctx, driverIDs)
	if err != nil {
		return riderCellHex, nil, fmt.Errorf("fetching driver states: %w", err)
	}

	candidates := make([]routing.Candidate, 0, len(candScores))
	for _, id := range driverIDs {
		state, ok := states[id]
		if !ok || state == nil {
			continue
		}
		if state.Status != "available" {
			continue
		}

		distM := geo.HaversineMeters(riderLat, riderLng, state.Lat, state.Lng)
		candidates = append(candidates, routing.Candidate{
			DriverID: id,
			Lat:      state.Lat,
			Lng:      state.Lng,
			CellHex:  state.Cell,
			DistM:    distM,
		})
	}

	return riderCellHex, candidates, nil
}
