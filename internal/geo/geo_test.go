package geo

import (
	"math"
	"testing"
)

const (
	// Dhaka landmark coordinates
	gulshanLat = 23.7925
	gulshanLng = 90.4078

	bananiLat = 23.7937
	bananiLng = 90.4043

	motijheelLat = 23.7330
	motijheelLng = 90.4172
)

func TestLatLngToCell(t *testing.T) {
	cell, err := LatLngToCell(gulshanLat, gulshanLng, DispatchResolution)
	if err != nil {
		t.Fatalf("unexpected error converting lat/lng to cell: %v", err)
	}

	hex := CellToHex(cell)
	if hex == "" {
		t.Fatal("expected non-empty hex string")
	}

	parsed, err := HexToCell(hex)
	if err != nil {
		t.Fatalf("failed parsing hex %s back to cell: %v", hex, err)
	}

	if parsed != cell {
		t.Fatalf("cell mismatch: got %v, expected %v", parsed, cell)
	}
}

func TestKRing(t *testing.T) {
	cell, err := LatLngToCell(gulshanLat, gulshanLng, DispatchResolution)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	k := DefaultKRing // k=1
	ring, err := KRing(cell, k)
	if err != nil {
		t.Fatalf("KRing error: %v", err)
	}

	// k=1 hexagonal ring must return exactly 7 cells (origin + 6 neighbors)
	if len(ring) != 7 {
		t.Fatalf("expected 7 cells for KRing k=1, got %d", len(ring))
	}

	foundOrigin := false
	for _, c := range ring {
		if c == cell {
			foundOrigin = true
			break
		}
	}
	if !foundOrigin {
		t.Fatal("KRing result does not contain origin cell")
	}
}

func TestCellToParent(t *testing.T) {
	dispCell, err := LatLngToCell(gulshanLat, gulshanLng, DispatchResolution)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	surgeCell, err := CellToParent(dispCell, SurgeResolution)
	if err != nil {
		t.Fatalf("CellToParent error: %v", err)
	}

	expectedSurgeCell, err := LatLngToCell(gulshanLat, gulshanLng, SurgeResolution)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if surgeCell != expectedSurgeCell {
		t.Fatalf("surge parent mismatch: got %v, expected %v", surgeCell, expectedSurgeCell)
	}
}

func TestHaversineMeters(t *testing.T) {
	distGulshanBanani := HaversineMeters(gulshanLat, gulshanLng, bananiLat, bananiLng)
	// Gulshan-2 to Banani is ~380-450 meters
	if distGulshanBanani < 300 || distGulshanBanani > 600 {
		t.Fatalf("expected distance between 300m and 600m, got %f", distGulshanBanani)
	}

	// Gulshan to Motijheel is ~6-8 km
	distGulshanMotijheel := HaversineMeters(gulshanLat, gulshanLng, motijheelLat, motijheelLng)
	if distGulshanMotijheel < 6000 || distGulshanMotijheel > 8000 {
		t.Fatalf("expected distance between 6000m and 8000m, got %f", distGulshanMotijheel)
	}

	// Same point distance must be 0
	distZero := HaversineMeters(gulshanLat, gulshanLng, gulshanLat, gulshanLng)
	if math.Abs(distZero) > 0.0001 {
		t.Fatalf("expected 0m distance for identical coordinates, got %f", distZero)
	}
}

func BenchmarkHaversineMeters(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = HaversineMeters(gulshanLat, gulshanLng, bananiLat, bananiLng)
	}
}

func BenchmarkLatLngToCell(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = LatLngToCell(gulshanLat, gulshanLng, DispatchResolution)
	}
}
