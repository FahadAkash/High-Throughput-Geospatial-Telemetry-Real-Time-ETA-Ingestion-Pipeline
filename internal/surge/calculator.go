package surge

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
)

const (
	// Default pricing parameters in Paisa (100 Paisa = 1 BDT)
	BaseFarePaisa         int64   = 5000 // 50.00 BDT
	RatePerKmPaisa        int64   = 2500 // 25.00 BDT per km
	RatePerMinutePaisa    int64   = 300  // 3.00 BDT per min
	MaxSurgeMultiplier    float64 = 2.5
	MinSurgeMultiplier    float64 = 1.0
	SurgeSensitivityAlpha float64 = 0.35 // Slope factor for demand/supply imbalance
)

// DemandReader interface defines access to cell demand counters.
type DemandReader interface {
	IncrementDemand(ctx context.Context, cellHex string, minuteBucket int64, ttl time.Duration) (int64, error)
	GetDemand(ctx context.Context, cellHex string, minuteBucket int64) (int64, error)
}

// Calculator handles per-cell surge multiplier calculation at resolution 7.
type Calculator struct {
	demandStore DemandReader
}

// NewCalculator constructs a surge calculator.
func NewCalculator(demandStore DemandReader) *Calculator {
	return &Calculator{demandStore: demandStore}
}

// GetMinuteBucket returns the current minute bucket for demand aggregation.
func GetMinuteBucket(t time.Time) int64 {
	return t.Unix() / 60
}

// RecordDemand increments the demand count for the surge cell (H3 resolution 7).
func (c *Calculator) RecordDemand(ctx context.Context, riderLat, riderLng float64, now time.Time) (string, int64, error) {
	surgeCell, err := geo.LatLngToCell(riderLat, riderLng, geo.SurgeResolution)
	if err != nil {
		return "", 0, fmt.Errorf("resolving surge cell: %w", err)
	}
	surgeCellHex := geo.CellToHex(surgeCell)

	bucket := GetMinuteBucket(now)
	if c.demandStore == nil {
		return surgeCellHex, 1, nil
	}

	count, err := c.demandStore.IncrementDemand(ctx, surgeCellHex, bucket, 5*time.Minute)
	if err != nil {
		return surgeCellHex, 1, fmt.Errorf("incrementing surge demand: %w", err)
	}
	return surgeCellHex, count, nil
}

// ComputeMultiplier calculates surge multiplier given demand and available driver supply.
// Pure logic: easily tested with zero network overhead.
func ComputeMultiplier(demand, supply int64) float64 {
	if demand <= 0 {
		return MinSurgeMultiplier
	}
	if supply <= 0 {
		return MaxSurgeMultiplier
	}

	ratio := float64(demand) / float64(supply)
	if ratio <= 1.0 {
		return MinSurgeMultiplier
	}

	// Dynamic sigmoid / linear response
	mult := 1.0 + SurgeSensitivityAlpha*(ratio-1.0)
	if mult > MaxSurgeMultiplier {
		mult = MaxSurgeMultiplier
	}
	if mult < MinSurgeMultiplier {
		mult = MinSurgeMultiplier
	}

	// Round to 2 decimal places
	return math.Round(mult*100) / 100
}

// EstimateFarePaisa calculates the final estimated fare in integer paisa (BDT).
// Rule: Units: distances are meters, durations are time.Duration, money is integer paisa (BDT).
func EstimateFarePaisa(distanceMeters float64, duration time.Duration, surgeMultiplier float64) int64 {
	distKm := distanceMeters / 1000.0
	durMin := duration.Minutes()

	base := float64(BaseFarePaisa)
	distCost := distKm * float64(RatePerKmPaisa)
	timeCost := durMin * float64(RatePerMinutePaisa)

	rawFare := (base + distCost + timeCost) * surgeMultiplier
	return int64(math.Round(rawFare))
}
