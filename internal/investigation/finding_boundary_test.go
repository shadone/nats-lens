package investigation

import (
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestCapturedFindingBoundaryAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested *int64
		returned  int64
		count     int32
		wantErr   bool
	}{
		{"known empty ignored", proto.Int64(0), 5, 0, true}, {"wrong boundary", proto.Int64(5), 6, 0, true}, {"empty boundary with rows", proto.Int64(0), 0, 1, true}, {"known empty", proto.Int64(0), 0, 0, false}, {"same boundary", proto.Int64(5), 5, 0, false}, {"unrequested", nil, 5, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkEventSelectionReply("ListFindingEpisodes", (&v1.ListFindingEpisodesRequest{ReadEpoch: tc.requested}).ProtoReflect(), (&v1.ListFindingEpisodesResponse{ReadEpoch: tc.returned, TotalCount: tc.count}).ProtoReflect())
			if (err != nil) != tc.wantErr {
				t.Fatalf("boundary validation error=%v want=%v", err, tc.wantErr)
			}
		})
	}
}
