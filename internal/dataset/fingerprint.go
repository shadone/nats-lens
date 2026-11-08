package dataset

import (
	"context"
	"io"
	"os"
)

// FingerprintDatabase hashes a trusted CLOSED file already validated and held
// open read-only by OpenDatabase. The reader's file lock must remain held so the
// hash and semantic reads describe the same stable file. The caller owns that DB.
// This bounded streaming digest establishes content identity, not authenticity,
// authorization, or a sandbox for malicious databases. No file or DB is modified.
func FingerprintDatabase(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > MaxDataBytes {
		return "", ErrBudget
	}
	f, err := os.Open(path)
	if err != nil {
		return "", ErrInvalid
	}
	defer f.Close()
	sum, size, err := digest(ctx, io.LimitReader(f, MaxDataBytes+1))
	if err != nil {
		return "", err
	}
	after, err := f.Stat()
	if err != nil || size != st.Size() || !os.SameFile(st, after) || after.Size() != size || !after.ModTime().Equal(st.ModTime()) {
		return "", ErrIntegrity
	}
	return sum, nil
}
