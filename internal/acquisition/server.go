// Package acquisition exposes an explicitly enabled, local filesystem-authorized
// administrative acquisition channel. It is separate from the read-only API.
package acquisition

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shadone/nats-lens/internal/dataset"
	"github.com/shadone/nats-lens/internal/store"
)

type Source struct {
	Store  *store.Store
	System dataset.System
}
type Options struct {
	Socket, Directory, ApplicationVersion string
	Budget                                dataset.Options
}

// Start returns only after the private socket is ready. Both directories must
// already exist, be owned by this user, be private, and contain no symlink in
// their configured final component. Existing socket paths are never removed.
func Start(ctx context.Context, o Options, sources []Source) (func(), error) {
	if o.Budget.MaxBytes < 0 || o.Budget.MaxBytes > dataset.MaxDataBytes || o.Budget.MinFreeBytes < 0 || o.Budget.MinFreeBytes > dataset.MaxDataBytes {
		return nil, dataset.ErrInvalid
	}
	if o.Socket == "" || o.Directory == "" {
		return nil, fmt.Errorf("acquisition socket and directory must be configured together")
	}
	socket, err := filepath.Abs(o.Socket)
	if err != nil {
		return nil, err
	}
	directory, err := filepath.Abs(o.Directory)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{filepath.Dir(socket), directory} {
		if err = privateOwnedDirectory(p); err != nil {
			return nil, err
		}
	}
	if _, err = os.Lstat(socket); !os.IsNotExist(err) {
		return nil, fmt.Errorf("acquisition socket path already exists; inspect stale socket manually")
	}
	byID := map[string]Source{}
	for _, s := range sources {
		if s.Store == nil || s.System.ID == "" {
			return nil, dataset.ErrInvalid
		}
		if _, ok := byID[s.System.ID]; ok {
			return nil, dataset.ErrInvalid
		}
		byID[s.System.ID] = s
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return nil, err
	}
	ln.SetUnlinkOnClose(false)
	info, err := os.Lstat(socket)
	if err != nil {
		ln.Close()
		return nil, err
	}
	clean := func() {
		ln.Close()
		if current, e := os.Lstat(socket); e == nil && os.SameFile(info, current) {
			_ = os.Remove(socket)
		}
	}
	if err = os.Chmod(socket, 0600); err != nil {
		clean()
		return nil, err
	}
	var callsMu sync.Mutex
	calls := 0
	closing := false
	idle := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		if closing {
			callsMu.Unlock()
			http.Error(w, "closing", http.StatusServiceUnavailable)
			return
		}
		calls++
		callsMu.Unlock()
		defer func() {
			callsMu.Lock()
			defer callsMu.Unlock()
			calls--
			if closing && calls == 0 {
				close(idle)
			}
		}()
		if r.Method != http.MethodPost || r.URL.Path != "/acquire" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
			return
		}
		var request struct {
			System string `json:"system"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		dec.DisallowUnknownFields()
		if dec.Decode(&request) != nil || dec.Decode(new(any)) != io.EOF {
			http.Error(w, "invalid acquisition request", http.StatusBadRequest)
			return
		}
		source, ok := byID[request.System]
		if !ok {
			http.Error(w, "unknown system", http.StatusBadRequest)
			return
		}
		destination := filepath.Join(directory, "dataset-"+rand.Text())
		m, err := dataset.Acquire(r.Context(), source.Store, destination, source.System, o.ApplicationVersion, o.Budget)
		if err != nil {
			status := http.StatusInternalServerError
			code := "acquisition_failed"
			switch {
			case errors.Is(err, dataset.ErrBusy):
				status = http.StatusConflict
				code = "busy"
			case errors.Is(err, dataset.ErrSpace):
				status = http.StatusInsufficientStorage
				code = "insufficient_space"
			case errors.Is(err, dataset.ErrBudget):
				status = http.StatusRequestEntityTooLarge
				code = "budget_exceeded"
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				status = http.StatusRequestTimeout
				code = "cancelled"
			}
			// Never expose native diagnostics, SQL, payloads, configured paths or secrets.
			http.Error(w, code, status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Result{Path: destination, Manifest: m})
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: dataset.MaxDuration + 15*time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 4096, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ln) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			callsMu.Lock()
			closing = true
			if calls == 0 {
				close(idle)
			}
			callsMu.Unlock()
			_ = srv.Close()
			<-done
			<-idle
			clean()
		})
	}
	// Each native job retains its global slot until it actually unwinds.
	return stop, nil
}

type Result struct {
	Path     string           `json:"path"`
	Manifest dataset.Manifest `json:"manifest"`
}

// Request explicitly asks the same-user local collector to acquire one system.
// It returns a server-local artifact path; it never copies or uploads the result.
func Request(ctx context.Context, socket, system string) (Result, error) {
	var result Result
	if system == "" {
		return result, dataset.ErrInvalid
	}
	if err := privateOwnedDirectory(filepath.Dir(socket)); err != nil {
		return result, err
	}
	info, err := os.Lstat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 {
		return result, dataset.ErrInvalid
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: dataset.MaxDuration + 20*time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	b, err := json.Marshal(struct {
		System string `json:"system"`
	}{system})
	if err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/acquire", bytes.NewReader(b))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, fmt.Errorf("acquisition cancelled: %w", err)
		}
		return result, fmt.Errorf("local acquisition request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
		return result, fmt.Errorf("acquisition: %s", string(b))
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, dataset.MaxManifestBytes+4096))
	if err = dec.Decode(&result); err != nil {
		return Result{}, fmt.Errorf("invalid acquisition response")
	}
	return result, nil
}
