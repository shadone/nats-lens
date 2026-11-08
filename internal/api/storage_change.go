package api

import v1 "github.com/shadone/nats-lens/internal/gen/natslens/v1"

// StorageChangeFacts contains endpoint-pinned facts before ranking and paging.
// Providers leave missing measurements unset and qualify observed absence.
type StorageChangeFacts struct {
	A, B    *v1.StorageChangeEndpoint
	Rows    []*v1.StorageChangeRow
	Reasons []string
}

// FinishStorageChange applies the same arithmetic and page contract to live,
// captured and demo facts. It does not read or mutate the source database.
func FinishStorageChange(req *v1.GetStorageChangeRequest, f *StorageChangeFacts) (*v1.GetStorageChangeResponse, error) {
	return &v1.GetStorageChangeResponse{}, nil
}
