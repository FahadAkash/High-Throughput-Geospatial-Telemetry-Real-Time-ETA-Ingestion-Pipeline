package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
)

var (
	etaDurationHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ride_eta_seconds",
		Help:    "Latency of OSRM routing matrix calculations in seconds.",
		Buckets: []float64{0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000},
	})
)

// Candidate represents a candidate driver for dispatch.
type Candidate struct {
	DriverID string
	Lat      float64
	Lng      float64
	CellHex  string
	DistM    float64
}

// CandidateETA holds computed driving duration and distance to pickup point.
type CandidateETA struct {
	Candidate Candidate
	Duration  time.Duration
	DistanceM float64
}

// Router interface for computing table matrix ETAs.
type Router interface {
	ComputeTable(
		ctx context.Context,
		riderLat, riderLng float64,
		riderCell string,
		candidates []Candidate,
		now time.Time,
	) ([]CandidateETA, error)
}

// OSRMClient implements Router against an OSRM routing backend with fallback heuristics.
type OSRMClient struct {
	baseURL    string
	httpClient *http.Client
	cache      *ETACache
}

// NewOSRMClient constructs an OSRMClient.
func NewOSRMClient(baseURL string, cache *ETACache) *OSRMClient {
	return &OSRMClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
		cache: cache,
	}
}

type osrmTableResponse struct {
	Code      string      `json:"code"`
	Durations [][]float64 `json:"durations"`
	Distances [][]float64 `json:"distances"`
	Message   string      `json:"message,omitempty"`
}

// ComputeTable executes a SINGLE OSRM /table call for all candidates to avoid N individual requests.
// RULE: Haversine prefilter to N nearest (default 10), then one OSRM table call for all candidates. Never one route call per driver.
func (c *OSRMClient) ComputeTable(
	ctx context.Context,
	riderLat, riderLng float64,
	riderCell string,
	candidates []Candidate,
	now time.Time,
) ([]CandidateETA, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	start := time.Now()
	defer func() {
		etaDurationHistogram.Observe(time.Since(start).Seconds())
	}()

	results := make([]CandidateETA, len(candidates))

	// 1. Check cache for each candidate's cell-to-cell ETA
	uncachedIndices := make([]int, 0, len(candidates))
	for i, cand := range candidates {
		results[i] = CandidateETA{
			Candidate: cand,
			DistanceM: cand.DistM,
		}

		if c.cache != nil {
			if duration, ok := c.cache.Get(ctx, cand.CellHex, riderCell, now); ok {
				results[i].Duration = duration
				continue
			}
		}
		uncachedIndices = append(uncachedIndices, i)
	}

	// If all were cached, return immediately
	if len(uncachedIndices) == 0 {
		return results, nil
	}

	// 2. Query OSRM /table API for uncached candidates
	// Format: /table/v1/driving/{d1Lng},{d1Lat};{d2Lng},{d2Lat};...;{riderLng},{riderLat}?sources=0,1..&destinations={riderIndex}
	coords := make([]string, len(uncachedIndices)+1)
	sources := make([]string, len(uncachedIndices))
	for idx, candIdx := range uncachedIndices {
		cand := candidates[candIdx]
		coords[idx] = fmt.Sprintf("%.6f,%.6f", cand.Lng, cand.Lat)
		sources[idx] = fmt.Sprintf("%d", idx)
	}
	riderIdx := len(uncachedIndices)
	coords[riderIdx] = fmt.Sprintf("%.6f,%.6f", riderLng, riderLat)

	reqURL := fmt.Sprintf("%s/table/v1/driving/%s?sources=%s&destinations=%d&annotations=duration,distance",
		c.baseURL,
		strings.Join(coords, ";"),
		strings.Join(sources, ";"),
		riderIdx,
	)

	resp, err := c.fetchOSRMTable(ctx, reqURL)
	if err != nil {
		// Fallback heuristic: calculate ETA using realistic Dhaka urban road parameters
		// Average urban car speed in Dhaka ~ 20-25 km/h (5.5-7 m/s) with 1.35x road circuity factor.
		c.fallbackHeuristic(riderLat, riderLng, uncachedIndices, candidates, results)
		return results, nil
	}

	// Populate durations from OSRM table response
	for idx, candIdx := range uncachedIndices {
		cand := candidates[candIdx]
		if idx < len(resp.Durations) && len(resp.Durations[idx]) > 0 {
			durSec := resp.Durations[idx][0]
			dur := time.Duration(durSec * float64(time.Second))
			results[candIdx].Duration = dur

			if idx < len(resp.Distances) && len(resp.Distances[idx]) > 0 {
				results[candIdx].DistanceM = resp.Distances[idx][0]
			}

			// Cache result for this cell-to-cell pair
			if c.cache != nil {
				c.cache.Set(ctx, cand.CellHex, riderCell, now, dur)
			}
		}
	}

	return results, nil
}

func (c *OSRMClient) fetchOSRMTable(ctx context.Context, requestURL string) (*osrmTableResponse, error) {
	parsedURL, err := url.Parse(requestURL)
	if err != nil {
		return nil, fmt.Errorf("parsing osrm url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("creating osrm request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling osrm table: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osrm table returned status %d", resp.StatusCode)
	}

	var tableResp osrmTableResponse
	if err := json.NewDecoder(resp.Body).Decode(&tableResp); err != nil {
		return nil, fmt.Errorf("decoding osrm json: %w", err)
	}

	if tableResp.Code != "Ok" {
		return nil, fmt.Errorf("osrm responded with error code: %s", tableResp.Code)
	}

	return &tableResp, nil
}

func (c *OSRMClient) fallbackHeuristic(
	riderLat, riderLng float64,
	uncachedIndices []int,
	candidates []Candidate,
	results []CandidateETA,
) {
	const (
		urbanSpeedMetersPerSec = 6.0  // approx 21.6 km/h (Dhaka average)
		circuityFactor         = 1.35 // road network routing circuity
	)

	for _, candIdx := range uncachedIndices {
		cand := candidates[candIdx]
		dist := geo.HaversineMeters(cand.Lat, cand.Lng, riderLat, riderLng)
		roadDist := dist * circuityFactor
		durSec := roadDist / urbanSpeedMetersPerSec
		results[candIdx].DistanceM = roadDist
		results[candIdx].Duration = time.Duration(durSec * float64(time.Second))
	}
}
