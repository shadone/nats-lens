package api_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/shadone/nats-lens/internal/api"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
)

// pairwiseOverlaps is the rule stated plainly: every other non-mirror
// stream of the same account with a subject that overlaps one of its own.
func pairwiseOverlaps(streams []api.SubjectStreamFact) map[[2]string][]string {
	out := map[[2]string][]string{}
	for _, s := range streams {
		if s.Mirror != "" {
			continue
		}
		for _, o := range streams {
			if o.Account != s.Account || o.Stream == s.Stream || o.Mirror != "" {
				continue
			}
			hit := false
			for _, x := range s.Subjects {
				for _, y := range o.Subjects {
					hit = hit || api.SubjectsOverlap(x, y)
				}
			}
			if hit {
				out[[2]string{s.Account, s.Stream}] = append(out[[2]string{s.Account, s.Stream}], o.Stream)
			}
		}
		slices.Sort(out[[2]string{s.Account, s.Stream}])
	}
	return out
}

func overlapsOfRows(res *v1.GetSubjectsResponse) map[[2]string][]string {
	out := map[[2]string][]string{}
	for _, r := range res.GetRows() {
		if r.GetStream() != "" && len(r.GetOverlaps()) > 0 {
			out[[2]string{r.GetAccount(), r.GetStream()}] = r.GetOverlaps()
		}
	}
	return out
}

func randomSubject(r *rand.Rand) string {
	tokens := []string{"a", "b", "c", "$KV", "*"}
	n := 1 + r.IntN(4)
	var parts []string
	for i := range n {
		if i == n-1 && r.IntN(4) == 0 {
			parts = append(parts, ">")
			break
		}
		parts = append(parts, tokens[r.IntN(len(tokens))])
	}
	return strings.Join(parts, ".")
}

// The indexed overlap check finds exactly the pairs the plain pairwise
// rule finds, on random mixes of literals, wildcards, KV-like names,
// mirrors and two accounts.
func TestSubjectsOverlapMatchesPairwise(t *testing.T) {
	for seed := range uint64(20) {
		r := rand.New(rand.NewPCG(seed, 7))
		var streams []api.SubjectStreamFact
		for i := range 150 {
			s := api.SubjectStreamFact{Account: []string{"APP", "SHOP"}[r.IntN(2)], Stream: fmt.Sprintf("s%d", i), Retention: "limits"}
			if r.IntN(5) == 0 {
				s.Stream = fmt.Sprintf("KV_s%d", i)
			}
			if r.IntN(10) == 0 {
				s.Mirror = "s0"
			} else {
				for range 1 + r.IntN(3) {
					s.Subjects = append(s.Subjects, randomSubject(r))
				}
			}
			streams = append(streams, s)
		}
		f := &api.SubjectsFacts{Epoch: 1, EpochAt: subjAt, Streams: streams}
		got := overlapsOfRows(api.FinishSubjects(f, "", v1.SubjectFilter_SUBJECT_FILTER_UNSPECIFIED))
		want := pairwiseOverlaps(streams)
		for k, v := range want {
			if len(v) == 0 {
				delete(want, k)
			}
		}
		if len(want) == 0 {
			t.Fatalf("seed %d: the mix has no overlap, so it tests nothing", seed)
		}
		for k, v := range want {
			if !slices.Equal(got[k], v) {
				t.Fatalf("seed %d, %v: overlaps %v, want %v", seed, k, got[k], v)
			}
		}
		for k, v := range got {
			if _, ok := want[k]; !ok {
				t.Fatalf("seed %d, %v: overlaps %v, want none", seed, k, v)
			}
		}
	}
}

// 10,000 KV buckets and streams in one account are checked for overlap
// well within a call's time: the rows come from Go after the provider's
// query, outside its timeout.
func TestSubjectsOverlapManyStreams(t *testing.T) {
	var streams []api.SubjectStreamFact
	for i := range 5000 {
		streams = append(streams,
			api.SubjectStreamFact{Account: "APP", Stream: fmt.Sprintf("KV_b%d", i), Retention: "limits", Subjects: []string{fmt.Sprintf("$KV.b%d.>", i)}},
			api.SubjectStreamFact{Account: "APP", Stream: fmt.Sprintf("orders-%d", i), Retention: "limits", Subjects: []string{fmt.Sprintf("orders.%d.*", i), fmt.Sprintf("audit.%d.>", i)}})
	}
	streams = append(streams, api.SubjectStreamFact{Account: "APP", Stream: "everything", Retention: "limits", Subjects: []string{"orders.>"}})
	f := &api.SubjectsFacts{Epoch: 1, EpochAt: subjAt, Streams: streams}
	start := time.Now()
	res := api.FinishSubjects(f, "", v1.SubjectFilter_SUBJECT_FILTER_UNSPECIFIED)
	took := time.Since(start)
	t.Logf("10,001 streams: %v", took)
	// About 20 ms on its own, and 358 ms under the race detector on a loaded
	// machine. Comparing every pair takes seconds, and about 44 s under the
	// race detector.
	bound := 200 * time.Millisecond
	if raceDetector {
		bound = 5 * time.Second
	}
	if took > bound {
		t.Fatalf("FinishSubjects took %v for 10,001 streams, want under %v", took, bound)
	}
	if got := res.GetOverlappingCount(); got != 5001 {
		t.Fatalf("overlapping rows %d, want 5,001 (every orders stream and the one on orders.>)", got)
	}
}

// 1,000 streams of 30 subjects each fit in one response: a row lists 20
// subjects and counts the rest.
func TestSubjectsResponseWithinSizeLimit(t *testing.T) {
	var streams []api.SubjectStreamFact
	for i := range 1000 {
		s := api.SubjectStreamFact{Account: "APP", Stream: fmt.Sprintf("orders-%04d", i), Retention: "limits"}
		for j := range 30 {
			s.Subjects = append(s.Subjects, fmt.Sprintf("orders.region-%02d.warehouse-%04d.>", j, i))
		}
		streams = append(streams, s)
	}
	res := api.FinishSubjects(&api.SubjectsFacts{Epoch: 1, EpochAt: subjAt, Streams: streams}, "", v1.SubjectFilter_SUBJECT_FILTER_UNSPECIFIED)
	size := proto.Size(res)
	t.Logf("1,000 rows of 30 subjects: %d bytes", size)
	if size > api.MaxMessageBytes {
		t.Fatalf("response is %d bytes, over the limit of %d", size, api.MaxMessageBytes)
	}
	i := slices.IndexFunc(res.GetRows(), func(r *v1.SubjectRow) bool { return r.GetStream() == "orders-0007" })
	if i < 0 {
		t.Fatal("no row for orders-0007")
	}
	r := res.GetRows()[i]
	if want := streams[7].Subjects[:api.MaxRowPatterns]; !slices.Equal(r.GetPatterns(), want) || r.GetMorePatterns() != 10 {
		t.Fatalf("orders-0007 lists %v and counts %d more, want the configuration's first 20, %v, and 10", r.GetPatterns(), r.GetMorePatterns(), want)
	}
}
