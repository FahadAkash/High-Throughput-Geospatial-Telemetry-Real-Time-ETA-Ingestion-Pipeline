package geo

const (
	// DispatchResolution is the Uber H3 resolution used for driver indexing and dispatch (approx. 460m edge).
	DispatchResolution = 8

	// SurgeResolution is the Uber H3 resolution used for aggregated demand/supply and surge pricing (approx. 1.2km edge).
	SurgeResolution = 7

	// DefaultKRing is the default number of hexagonal rings searched around a cell (k=1 yields 7 cells total).
	DefaultKRing = 1

	// EarthRadiusMeters is the approximate mean radius of the Earth in meters.
	EarthRadiusMeters = 6371000.0
)
