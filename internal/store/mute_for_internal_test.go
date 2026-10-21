package store

import "testing"

// An acceptance of the finding's entity wins over a plain mute of it,
// whichever is newer; among plain mutes the newest wins; one for the
// entity wins over one for the whole check.
func TestMuteForPrefersTheAcceptance(t *testing.T) {
	f := Finding{CheckCode: "stream-005", EntityType: "stream", EntityKey: "APP/order-archive", State: StateFiring}
	plain := func(id int64) Mute {
		return Mute{ID: id, CheckCode: "stream-005", EntityType: "stream", EntityKey: "APP/order-archive"}
	}
	accept := Mute{ID: 2, CheckCode: "stream-005", EntityType: "stream", EntityKey: "APP/order-archive", Accepted: true}
	whole := Mute{ID: 9, CheckCode: "stream-005"}
	for _, c := range []struct {
		name  string
		mutes []Mute // newest first
		want  int64
	}{
		{"acceptance older", []Mute{whole, plain(3), accept, plain(1)}, 2},
		{"acceptance newer", []Mute{accept, plain(1)}, 2},
		{"plain mutes", []Mute{plain(3), plain(1)}, 3},
		{"whole check only", []Mute{whole}, 9},
	} {
		if m := muteFor(c.mutes, f); m == nil || m.ID != c.want {
			t.Errorf("%s: %v, want mute %d", c.name, m, c.want)
		}
	}
}
