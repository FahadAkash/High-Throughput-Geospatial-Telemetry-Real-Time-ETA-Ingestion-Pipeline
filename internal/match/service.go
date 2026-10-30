package match

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/routing"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/postgres"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/surge"
)

var (
	matchLatencyHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ride_match_seconds",
		Help:    "End-to-end request-to-match latency in seconds.",
		Buckets: []float64{0.002, 0.005, 0.010, 0.020, 0.050, 0.100, 0.250, 0.500, 1.000},
	})
)

// Clock provides injectable UTC time for deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

// RideRepository interface for storing completed matches in PostgreSQL.
type RideRepository interface {
	CreateRide(ctx context.Context, ride *postgres.Ride) error
}

// MatchRequest represents an incoming dispatch request from a rider.
type MatchRequest struct {
	RideID     string  `json:"ride_id"`
	RiderID    string  `json:"rider_id"`
	PickupLat  float64 `json:"pickup_lat"`
	PickupLng  float64 `json:"pickup_lng"`
	DropoffLat float64 `json:"dropoff_lat"`
	DropoffLng float64 `json:"dropoff_lng"`
}

// MatchResponse returns the matched driver, ETAs, fare, and surge status.
type MatchResponse struct {
	RideID          string        `json:"ride_id"`
	DriverID        string        `json:"driver_id"`
	PickupETA       time.Duration `json:"pickup_eta"`
	DistanceM       float64       `json:"distance_meters"`
	FarePaisa       int64         `json:"fare_paisa"`
	SurgeMultiplier float64       `json:"surge_multiplier"`
	MatchedAt       time.Time     `json:"matched_at"`
}

// Service orchestrates the complete match lifecycle.
type Service struct {
	candidateReader CandidateReader
	claimer         DriverClaimer
	router          routing.Router
	surgeCalc       *surge.Calculator
	repo            RideRepository
	clock           Clock
	staleWindow     time.Duration
	claimTTL        time.Duration
	maxCandidates   int
}

// NewService constructs a dispatch match service.
func NewService(
	reader CandidateReader,
	claimer DriverClaimer,
	router routing.Router,
	surgeCalc *surge.Calculator,
	repo RideRepository,
	clock Clock,
	staleWindow time.Duration,
	claimTTL time.Duration,
	maxCandidates int,
) *Service {
	if clock == nil {
		clock = realClock{}
	}
	if maxCandidates <= 0 {
		maxCandidates = 10
	}
	if claimTTL <= 0 {
		claimTTL = 10 * time.Second
	}
	return &Service{
		candidateReader: reader,
		claimer:         claimer,
		router:          router,
		surgeCalc:       surgeCalc,
		repo:            repo,
		clock:           clock,
		staleWindow:     staleWindow,
		claimTTL:        claimTTL,
		maxCandidates:   maxCandidates,
	}
}

// Match dispatches a rider to the optimal nearby driver:
// 1. K-ring candidate search in Redis.
// 2. Haversine distance prefilter & OSRM table matrix ETA ranking.
// 3. Dynamic surge calculation based on demand/supply.
// 4. Atomic driver claim (NX PX lock).
// 5. Short transaction ride write to PostgreSQL.
func (s *Service) Match(ctx context.Context, req *MatchRequest) (*MatchResponse, error) {
	start := time.Now()
	defer func() {
		matchLatencyHistogram.Observe(time.Since(start).Seconds())
	}()

	now := s.clock.Now()

	// 1. K-Ring Candidate Search around pickup location
	riderCellHex, candidates, err := FindCandidates(ctx, s.candidateReader, req.PickupLat, req.PickupLng, s.staleWindow, now)
	if err != nil {
		return nil, fmt.Errorf("finding candidates: %w", err)
	}

	if len(candidates) == 0 {
		return nil, ErrNoDriversAvailable
	}

	// 2. Rank candidates (Haversine prefilter -> OSRM Table -> Sort by Duration)
	rankedETAs, err := RankCandidates(ctx, s.router, req.PickupLat, req.PickupLng, riderCellHex, candidates, s.maxCandidates, now)
	if err != nil {
		return nil, fmt.Errorf("ranking candidates: %w", err)
	}

	// 3. Surge Pricing Calculation (Demand vs Available Supply)
	var surgeMult float64 = 1.0
	if s.surgeCalc != nil {
		_, demandCount, _ := s.surgeCalc.RecordDemand(ctx, req.PickupLat, req.PickupLng, now)
		supplyCount := int64(len(candidates))
		surgeMult = surge.ComputeMultiplier(demandCount, supplyCount)
	}

	// 4. Atomic Driver Claim
	claimed, err := ClaimBestDriver(ctx, s.claimer, req.RideID, rankedETAs, s.claimTTL)
	if err != nil {
		return nil, fmt.Errorf("claiming candidate: %w", err)
	}

	// 5. Trip Distance & Fare Calculation
	tripDistM := geo.HaversineMeters(req.PickupLat, req.PickupLng, req.DropoffLat, req.DropoffLng)
	tripDurationEst := time.Duration((tripDistM / 6.0) * float64(time.Second)) // ~21.6 km/h speed
	farePaisa := surge.EstimateFarePaisa(tripDistM, tripDurationEst, surgeMult)

	// 6. Write ride record to PostgreSQL (hot-path claim is done; this commits ride state)
	if s.repo != nil {
		rideRecord := &postgres.Ride{
			RideID:          req.RideID,
			RiderID:         req.RiderID,
			DriverID:        claimed.Candidate.DriverID,
			PickupLat:       req.PickupLat,
			PickupLng:       req.PickupLng,
			DropoffLat:      req.DropoffLat,
			DropoffLng:      req.DropoffLng,
			FarePaisa:       farePaisa,
			SurgeMultiplier: surgeMult,
			Status:          "assigned",
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := s.repo.CreateRide(ctx, rideRecord); err != nil {
			// If Postgres insert fails, release claim lock so driver is not stranded
			_, _ = s.claimer.ReleaseClaim(ctx, claimed.Candidate.DriverID, req.RideID)
			return nil, fmt.Errorf("persisting ride record: %w", err)
		}
	}

	return &MatchResponse{
		RideID:          req.RideID,
		DriverID:        claimed.Candidate.DriverID,
		PickupETA:       claimed.Duration,
		DistanceM:       claimed.DistanceM,
		FarePaisa:       farePaisa,
		SurgeMultiplier: surgeMult,
		MatchedAt:       now,
	}, nil
}
