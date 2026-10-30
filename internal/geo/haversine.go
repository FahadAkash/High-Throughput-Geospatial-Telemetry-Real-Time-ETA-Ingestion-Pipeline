package geo

import "math"

// HaversineMeters calculates the great-circle distance between two points on Earth in meters.
// Highly optimized for zero heap allocations on the hot path.
func HaversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const degToRad = math.Pi / 180.0

	dLat := (lat2 - lat1) * degToRad
	dLon := (lon2 - lon1) * degToRad

	lat1Rad := lat1 * degToRad
	lat2Rad := lat2 * degToRad

	sinHalfDLat := math.Sin(dLat / 2.0)
	sinHalfDLon := math.Sin(dLon / 2.0)

	a := sinHalfDLat*sinHalfDLat + math.Cos(lat1Rad)*math.Cos(lat2Rad)*sinHalfDLon*sinHalfDLon
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))

	return EarthRadiusMeters * c
}
