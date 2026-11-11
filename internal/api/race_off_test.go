//go:build !race

package api_test

// raceDetector is true when the tests run with the race detector, which
// makes CPU-bound code several times slower.
const raceDetector = false
