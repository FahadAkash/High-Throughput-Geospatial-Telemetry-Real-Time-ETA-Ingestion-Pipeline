package geo

import (
	"fmt"

	"github.com/uber/h3-go/v4"
)

// Cell represents a 64-bit Uber H3 geospatial hexagon index.
type Cell = h3.Cell

// LatLngToCell converts latitude and longitude coordinates to an H3 Cell at the specified resolution.
func LatLngToCell(lat, lng float64, res int) (Cell, error) {
	coord := h3.LatLng{
		Lat: lat,
		Lng: lng,
	}
	cell, err := h3.LatLngToCell(coord, res)
	if err != nil {
		return 0, fmt.Errorf("converting lat/lng (%f, %f) to cell at res %d: %w", lat, lng, res, err)
	}
	if !cell.IsValid() {
		return 0, fmt.Errorf("invalid coordinates (%f, %f) for resolution %d", lat, lng, res)
	}
	return cell, nil
}

// CellToHex converts an H3 Cell to its hexadecimal string representation.
func CellToHex(cell Cell) string {
	return cell.String()
}

// HexToCell parses a hexadecimal string into an H3 Cell.
func HexToCell(hex string) (Cell, error) {
	cell := h3.CellFromString(hex)
	if !cell.IsValid() {
		return 0, fmt.Errorf("invalid H3 cell hex: %q", hex)
	}
	return cell, nil
}

// KRing returns the origin cell and all neighbor cells within k hexagonal rings.
// For k=1, returns 7 cells (origin + 6 immediate neighbors).
func KRing(cell Cell, k int) ([]Cell, error) {
	if !cell.IsValid() {
		return nil, fmt.Errorf("cannot calculate KRing for invalid cell: %v", cell)
	}
	cells, err := h3.GridDisk(cell, k)
	if err != nil {
		return nil, fmt.Errorf("calculating grid disk for cell %v (k=%d): %w", cell, k, err)
	}
	return cells, nil
}

// CellToParent rolls up an H3 cell to a coarser parent resolution (e.g. res 8 to res 7 for surge).
func CellToParent(cell Cell, parentRes int) (Cell, error) {
	if !cell.IsValid() {
		return 0, fmt.Errorf("cannot get parent for invalid cell: %v", cell)
	}
	parent, err := cell.Parent(parentRes)
	if err != nil {
		return 0, fmt.Errorf("obtaining parent cell at res %d: %w", parentRes, err)
	}
	return parent, nil
}
