package fake

// maxMadeRecords bounds the mutes, and separately the acknowledgements,
// that a demo keeps from API calls, so a demo others can reach does not
// grow without bound in memory.
const maxMadeRecords = 500

// keepNewest drops the oldest records after the first seeded ones, so that
// at most maxMadeRecords made through the API remain. The order is kept.
func keepNewest[T any](s []T, seeded int) []T {
	if extra := len(s) - seeded - maxMadeRecords; extra > 0 {
		s = append(s[:seeded], s[seeded+extra:]...)
	}
	return s
}
