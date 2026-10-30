package match

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/routing"
)

// RankCandidates filters candidates to the top N nearest via Haversine distance,
// evaluates driving ETA via a single batch OSRM table call, and sorts by driving ETA.
// Invariant: Rank by distance/ETA, NOT by Redis score (score is timestamp, not proximity).
func RankCandidates(
	ctx context.Context,
	router routing.Router,
	riderLat, riderLng float64,
	riderCellHex string,
	candidates []routing.Candidate,
	maxCandidates int,
	now time.Time,
) ([]routing.CandidateETA, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	// 1. Sort all candidates by Haversine distance ascending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].DistM < candidates[j].DistM
	})

	// 2. Prefilter to top N nearest (default 10)
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	// 3. Batch query driving ETA via single table matrix call
	etas, err := router.ComputeTable(ctx, riderLat, riderLng, riderCellHex, candidates, now)
	if err != nil {
		return nil, fmt.Errorf("routing table calculation: %w", err)
	}

	// 4. Final ranking by driving duration (shortest pickup ETA first)
	sort.Slice(etas, func(i, j int) bool {
		if etas[i].Duration == etas[j].Duration {
			return etas[i].DistanceM < etas[j].DistanceM
		}
		return etas[i].Duration < etas[j].Duration
	})

	return etas, nil
}
