package match

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/routing"
)

var (
	claimConflictsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ride_claim_conflicts_total",
		Help: "Total count of driver claim conflicts encountered during dispatch.",
	})
)

var (
	ErrNoDriversAvailable = errors.New("no drivers available in range")
	ErrAllDriversConflict = errors.New("all candidate drivers could not be claimed due to concurrency conflicts")
)

// DriverClaimer defines interface to execute atomic claim locks.
type DriverClaimer interface {
	ClaimDriver(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error)
	ReleaseClaim(ctx context.Context, driverID, rideID string) (bool, error)
}

// ClaimBestDriver iterates through ranked candidates, attempting atomic claim.
// If another rider simultaneously claimed a driver, records conflict and falls back to next ranked candidate.
func ClaimBestDriver(
	ctx context.Context,
	claimer DriverClaimer,
	rideID string,
	rankedETAs []routing.CandidateETA,
	claimTTL time.Duration,
) (*routing.CandidateETA, error) {
	if len(rankedETAs) == 0 {
		return nil, ErrNoDriversAvailable
	}

	for _, candETA := range rankedETAs {
		driverID := candETA.Candidate.DriverID
		won, err := claimer.ClaimDriver(ctx, driverID, rideID, claimTTL)
		if err != nil {
			return nil, fmt.Errorf("claiming driver %s: %w", driverID, err)
		}

		if won {
			// Successfully acquired atomic mutex lock on driver
			return &candETA, nil
		}

		// Conflict: Driver was claimed by another concurrent rider request
		claimConflictsTotal.Inc()
	}

	return nil, ErrAllDriversConflict
}
