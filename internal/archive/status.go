package archive

import (
	"sync"
	"time"
)

// Status is the archive as the archiver last saw it: the hours of its
// last listing of the manifests, with the exports since, and the last
// export's time and error. The zero value is before the first listing
// and the first export.
type Status struct {
	// ListedAt is when the manifests were last listed; zero before the
	// first listing, when OldestHour, NewestHour and Hours are unknown.
	ListedAt time.Time
	// OldestHour and NewestHour bound the hours with a manifest, Hours
	// counts them.
	OldestHour, NewestHour time.Time
	Hours                  int
	// NewestFinal is whether the newest hour was exported after it closed.
	NewestFinal bool
	// LastExportAt is when the newest hour export finished.
	LastExportAt time.Time
	// LastError is the newest export error since the last export that
	// succeeded, at LastErrorAt; empty when there is none.
	LastError   string
	LastErrorAt time.Time
}

// status keeps the Status for readers on other goroutines.
type status struct {
	mu sync.Mutex
	// hours are the hours with a manifest, of the last listing and the
	// exports since.
	hours       map[time.Time]bool
	newestFinal bool
	s           Status
}

// Status returns the archive's state as of the last listing and export.
func (a *Archiver) Status() Status {
	st := &a.status
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.s
	out.Hours, out.NewestFinal = len(st.hours), st.newestFinal
	for h := range st.hours {
		if out.OldestHour.IsZero() || h.Before(out.OldestHour) {
			out.OldestHour = h
		}
		if h.After(out.NewestHour) {
			out.NewestHour = h
		}
	}
	return out
}

// listed records a listing of the manifests: the hours, and whether the
// newest was final.
func (st *status) listed(at time.Time, hours []time.Time, newestFinal bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.hours = make(map[time.Time]bool, len(hours))
	for _, h := range hours {
		st.hours[hourKey(h)] = true
	}
	st.newestFinal, st.s.ListedAt = newestFinal, at.UTC()
}

// exported records an hour exported at now. Before the first listing the
// hours stay unknown.
func (st *status) exported(hour time.Time, final bool, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.s.LastExportAt = now.UTC()
	if st.hours == nil {
		return
	}
	newest := true
	for h := range st.hours {
		if h.After(hour) {
			newest = false
		}
	}
	st.hours[hourKey(hour)] = true
	if newest {
		st.newestFinal = final
	}
}

// failed records an export error, or clears it when err is nil.
func (st *status) failed(err error, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err == nil {
		st.s.LastError, st.s.LastErrorAt = "", time.Time{}
		return
	}
	st.s.LastError, st.s.LastErrorAt = err.Error(), now.UTC()
}

// hourKey is an hour as a map key: in UTC, without a monotonic reading.
func hourKey(t time.Time) time.Time { return t.UTC().Round(0) }
