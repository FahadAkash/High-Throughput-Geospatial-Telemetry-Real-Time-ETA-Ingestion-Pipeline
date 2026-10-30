package redis

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

//go:embed lua/update_location.lua
var updateLocationScriptSrc string

//go:embed lua/claim_release.lua
var claimReleaseScriptSrc string

//go:embed lua/purge_stale.lua
var purgeStaleScriptSrc string

var (
	updateLocationScript = redis.NewScript(updateLocationScriptSrc)
	claimReleaseScript   = redis.NewScript(claimReleaseScriptSrc)
	purgeStaleScript     = redis.NewScript(purgeStaleScriptSrc)
)

// DriverState captures live cached driver telemetry in Redis.
type DriverState struct {
	DriverID string  `json:"driver_id"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Cell     string  `json:"cell"`
	Ts       int64   `json:"ts"`
	Status   string  `json:"status"`
}

// Client provides typed access to Redis hot-path state.
type Client struct {
	rdb redis.UniversalClient
}

// NewClient initializes a new Redis client with embedded Lua scripts preloaded.
func NewClient(rdb redis.UniversalClient) *Client {
	return &Client{rdb: rdb}
}

// Key formatting functions adhering strictly to AGENTS.md schema.
func CellKey(h3Hex string) string {
	return "cell:" + h3Hex
}

func DrvKey(driverID string) string {
	return "drv:" + driverID
}

func ClaimKey(driverID string) string {
	return "claim:" + driverID
}

func DemandKey(h3Hex string, minuteBucket int64) string {
	return fmt.Sprintf("demand:%s:%d", h3Hex, minuteBucket)
}

func ETAKey(origHex, destHex string, bucket5m int64) string {
	return fmt.Sprintf("eta:%s:%s:%d", origHex, destHex, bucket5m)
}

// UpdateLocation atomically updates driver cell ZSETs and drv hash via Lua script.
func (c *Client) UpdateLocation(
	ctx context.Context,
	driverID string,
	lat, lng float64,
	newCellHex, oldCellHex string,
	tsMs int64,
	status string,
	ttl time.Duration,
) error {
	newKey := CellKey(newCellHex)
	var oldKey string
	if oldCellHex != "" && oldCellHex != newCellHex {
		oldKey = CellKey(oldCellHex)
	}
	drvKey := DrvKey(driverID)

	keys := []string{newKey, oldKey, drvKey}
	args := []interface{}{
		driverID,
		strconv.FormatInt(tsMs, 10),
		strconv.FormatFloat(lat, 'f', 6, 64),
		strconv.FormatFloat(lng, 'f', 6, 64),
		newCellHex,
		status,
		strconv.FormatInt(ttl.Milliseconds(), 10),
	}

	err := updateLocationScript.Run(ctx, c.rdb, keys, args...).Err()
	if err != nil {
		return fmt.Errorf("running update_location script for driver %s: %w", driverID, err)
	}
	return nil
}

// ClaimDriver performs an atomic SET NX PX to lock a driver for a ride.
// Returns true if successfully claimed, false if already claimed by another rider.
func (c *Client) ClaimDriver(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error) {
	key := ClaimKey(driverID)
	ok, err := c.rdb.SetNX(ctx, key, rideID, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("claiming driver %s for ride %s: %w", driverID, rideID, err)
	}
	return ok, nil
}

// ReleaseClaim releases an existing driver claim atomically using compare-and-delete.
func (c *Client) ReleaseClaim(ctx context.Context, driverID, rideID string) (bool, error) {
	key := ClaimKey(driverID)
	res, err := claimReleaseScript.Run(ctx, c.rdb, []string{key}, rideID).Int()
	if err != nil {
		return false, fmt.Errorf("releasing claim for driver %s: %w", driverID, err)
	}
	return res == 1, nil
}

// PurgeStaleDrivers executes ZREMRANGEBYSCORE across cell keys to eliminate stale pings.
func (c *Client) PurgeStaleDrivers(ctx context.Context, cellHexes []string, maxStaleTs int64) (int64, error) {
	if len(cellHexes) == 0 {
		return 0, nil
	}
	keys := make([]string, len(cellHexes))
	for i, hex := range cellHexes {
		keys[i] = CellKey(hex)
	}

	res, err := purgeStaleScript.Run(ctx, c.rdb, keys, strconv.FormatInt(maxStaleTs, 10)).Int64()
	if err != nil {
		return 0, fmt.Errorf("purging stale drivers across %d cells: %w", len(cellHexes), err)
	}
	return res, nil
}

// CandidateScore holds driver ID and last ping timestamp from a cell ZSET.
type CandidateScore struct {
	DriverID string
	TsMs     int64
}

// GetActiveDriversInCells reads all drivers in the given cells with score >= minValidTs in one pipelined round trip.
func (c *Client) GetActiveDriversInCells(ctx context.Context, cellHexes []string, minValidTs int64) ([]CandidateScore, error) {
	if len(cellHexes) == 0 {
		return nil, nil
	}

	pipe := c.rdb.Pipeline()
	cmds := make([]*redis.StringSliceCmd, len(cellHexes))
	minScore := strconv.FormatInt(minValidTs, 10)

	for i, hex := range cellHexes {
		key := CellKey(hex)
		cmds[i] = pipe.ZRangeByScore(ctx, key, &redis.ZRangeBy{
			Min: minScore,
			Max: "+inf",
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("pipelined ZRangeByScore across %d cells: %w", len(cellHexes), err)
	}

	seen := make(map[string]bool)
	var candidates []CandidateScore

	for _, cmd := range cmds {
		members, err := cmd.Result()
		if err != nil {
			continue
		}
		for _, driverID := range members {
			if !seen[driverID] {
				seen[driverID] = true
				candidates = append(candidates, CandidateScore{DriverID: driverID})
			}
		}
	}

	return candidates, nil
}

// GetDriverStates fetches driver coordinate metadata hashes in a pipelined batch.
func (c *Client) GetDriverStates(ctx context.Context, driverIDs []string) (map[string]*DriverState, error) {
	if len(driverIDs) == 0 {
		return map[string]*DriverState{}, nil
	}

	pipe := c.rdb.Pipeline()
	cmds := make(map[string]*redis.MapStringStringCmd, len(driverIDs))
	for _, id := range driverIDs {
		cmds[id] = pipe.HGetAll(ctx, DrvKey(id))
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("pipelined HGetAll for %d drivers: %w", len(driverIDs), err)
	}

	result := make(map[string]*DriverState, len(driverIDs))
	for id, cmd := range cmds {
		fields, err := cmd.Result()
		if err != nil || len(fields) == 0 {
			continue
		}
		lat, _ := strconv.ParseFloat(fields["lat"], 64)
		lng, _ := strconv.ParseFloat(fields["lng"], 64)
		ts, _ := strconv.ParseInt(fields["ts"], 10, 64)

		result[id] = &DriverState{
			DriverID: id,
			Lat:      lat,
			Lng:      lng,
			Cell:     fields["cell"],
			Ts:       ts,
			Status:   fields["status"],
		}
	}

	return result, nil
}

// IncrementDemand increments the demand counter for an H3 cell minute bucket.
func (c *Client) IncrementDemand(ctx context.Context, cellHex string, minuteBucket int64, ttl time.Duration) (int64, error) {
	key := DemandKey(cellHex, minuteBucket)
	pipe := c.rdb.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("incrementing demand for cell %s: %w", cellHex, err)
	}
	return incrCmd.Val(), nil
}

// GetDemand returns the demand count for an H3 cell minute bucket.
func (c *Client) GetDemand(ctx context.Context, cellHex string, minuteBucket int64) (int64, error) {
	key := DemandKey(cellHex, minuteBucket)
	val, err := c.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("getting demand for cell %s: %w", cellHex, err)
	}
	return val, nil
}

// GetCachedETA retrieves a cached ETA duration in seconds if present.
func (c *Client) GetCachedETA(ctx context.Context, origHex, destHex string, bucket5m int64) (int64, bool, error) {
	key := ETAKey(origHex, destHex, bucket5m)
	val, err := c.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("getting cached ETA %s: %w", key, err)
	}
	return val, true, nil
}

// SetCachedETA stores a calculated ETA in the 5-minute bucket cache.
func (c *Client) SetCachedETA(ctx context.Context, origHex, destHex string, bucket5m int64, durationSec int64, ttl time.Duration) error {
	key := ETAKey(origHex, destHex, bucket5m)
	err := c.rdb.Set(ctx, key, durationSec, ttl).Err()
	if err != nil {
		return fmt.Errorf("setting cached ETA %s: %w", key, err)
	}
	return nil
}
