package storeprov

// SetReplayRowLimit lowers the Replay read limit for a test and returns
// the function that puts it back.
func SetReplayRowLimit(n int) func() {
	old := replayRowLimit
	replayRowLimit = n
	return func() { replayRowLimit = old }
}
