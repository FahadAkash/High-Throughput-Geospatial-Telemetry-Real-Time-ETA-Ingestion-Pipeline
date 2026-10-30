//go:build integration

package redis

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newTestRedisClient(t *testing.T) *Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Skipping integration test: Redis not accessible on 127.0.0.1:6379 (%v)", err)
	}

	return NewClient(rdb)
}

func TestLuaCellChange(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	driverID := fmt.Sprintf("drv_test_%d", time.Now().UnixNano())
	oldCell := "8865616521fffff"
	newCell := "8865616523fffff"
	nowMs := time.Now().UnixMilli()

	// 1. Initial Ping in oldCell
	err := client.UpdateLocation(ctx, driverID, 23.7925, 90.4078, oldCell, "", nowMs, "available", 30*time.Second)
	if err != nil {
		t.Fatalf("first UpdateLocation failed: %v", err)
	}

	// Verify driver is in oldCell
	drivers, err := client.GetActiveDriversInCells(ctx, []string{oldCell}, nowMs-5000)
	if err != nil || len(drivers) != 1 || drivers[0].DriverID != driverID {
		t.Fatalf("expected driver in oldCell, got: %v", drivers)
	}

	// 2. Move to newCell
	nextMs := nowMs + 1000
	err = client.UpdateLocation(ctx, driverID, 23.7937, 90.4043, newCell, oldCell, nextMs, "available", 30*time.Second)
	if err != nil {
		t.Fatalf("second UpdateLocation failed: %v", err)
	}

	// Verify driver removed from oldCell
	driversOld, err := client.GetActiveDriversInCells(ctx, []string{oldCell}, nowMs-5000)
	if err != nil || len(driversOld) != 0 {
		t.Fatalf("driver was not removed from oldCell: %v", driversOld)
	}

	// Verify driver present in newCell
	driversNew, err := client.GetActiveDriversInCells(ctx, []string{newCell}, nowMs-5000)
	if err != nil || len(driversNew) != 1 || driversNew[0].DriverID != driverID {
		t.Fatalf("expected driver in newCell, got: %v", driversNew)
	}

	// Verify metadata hash
	states, err := client.GetDriverStates(ctx, []string{driverID})
	if err != nil || states[driverID] == nil {
		t.Fatalf("driver state hash missing: %v", err)
	}
	state := states[driverID]
	if state.Cell != newCell || state.Lat != 23.7937 || state.Lng != 90.4043 {
		t.Fatalf("mismatched driver state: %+v", state)
	}
}

func TestLuaStaleDriverPurge(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	cell := fmt.Sprintf("88656165%x", time.Now().UnixNano()%0xFFFFFFF)
	d1 := "drv_stale_1"
	d2 := "drv_fresh_2"

	staleTs := time.Now().UnixMilli() - 20000 // 20s ago
	freshTs := time.Now().UnixMilli()         // now

	_ = client.UpdateLocation(ctx, d1, 23.7925, 90.4078, cell, "", staleTs, "available", 30*time.Second)
	_ = client.UpdateLocation(ctx, d2, 23.7930, 90.4080, cell, "", freshTs, "available", 30*time.Second)

	// Threshold: 15s ago
	threshold := time.Now().UnixMilli() - 15000
	removed, err := client.PurgeStaleDrivers(ctx, []string{cell}, threshold)
	if err != nil {
		t.Fatalf("PurgeStaleDrivers failed: %v", err)
	}
	if removed < 1 {
		t.Fatalf("expected at least 1 driver purged, got %d", removed)
	}

	// Query active drivers >= threshold
	active, err := client.GetActiveDriversInCells(ctx, []string{cell}, threshold)
	if err != nil {
		t.Fatalf("GetActiveDriversInCells failed: %v", err)
	}
	if len(active) != 1 || active[0].DriverID != d2 {
		t.Fatalf("expected only fresh driver %s, got: %v", d2, active)
	}
}

func TestLuaClaimContention(t *testing.T) {
	client := newTestRedisClient(t)
	ctx := context.Background()

	driverID := fmt.Sprintf("drv_claim_%d", time.Now().UnixNano())
	const numContenders = 20

	var winCount int32
	var wg sync.WaitGroup
	wg.Add(numContenders)

	for i := 0; i < numContenders; i++ {
		rideID := fmt.Sprintf("ride_%d", i)
		go func(rID string) {
			defer wg.Done()
			won, err := client.ClaimDriver(ctx, driverID, rID, 5*time.Second)
			if err == nil && won {
				atomic.AddInt32(&winCount, 1)
			}
		}(rideID)
	}

	wg.Wait()

	// Exactly ONE goroutine must win the claim
	if winCount != 1 {
		t.Fatalf("claim contention invariant broken: expected exactly 1 winner, got %d", winCount)
	}
}
