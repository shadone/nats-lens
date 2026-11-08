package investigation

import (
	"errors"
	v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// bindStorageComparison transports explicit IDs supplied by this RPC into its
// replay request. The actual sent request remains separately recorded. No metric
// calculation, source lookup or universal cross-RPC pin is introduced here.
func bindStorageComparison(request, response []byte) ([]byte, error) {
	req := &v1.GetStorageChangeRequest{}
	res := &v1.GetStorageChangeResponse{}
	if protojson.Unmarshal(request, req) != nil || protojson.Unmarshal(response, res) != nil {
		return nil, errors.New("storage comparison response does not match the supported contract")
	}
	if res.System != "" && req.System != "" && res.System != req.System || res.Account != req.Account {
		return nil, errors.New("storage comparison response scope differs from the request")
	}
	if req.System == "" {
		req.System = res.System
	}
	if req.System == "" {
		return nil, errors.New("storage comparison requires an explicit resolved system for replay")
	}
	if req.EpochA != 0 && req.EpochA != res.GetA().GetEpoch() || req.EpochB != 0 && req.EpochB != res.GetB().GetEpoch() {
		return nil, errors.New("storage comparison response changed a requested pin")
	}
	if res.GetA().GetEpoch() < 0 || res.GetB().GetEpoch() < 0 {
		return nil, errors.New("storage comparison response has an invalid collection pin")
	}
	req.EpochA = res.GetA().GetEpoch()
	req.EpochB = res.GetB().GetEpoch()
	return protojson.Marshal(req)
}
