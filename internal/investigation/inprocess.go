package investigation

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"
)

// NewInProcess sends the same bounded protobuf JSON calls through an application
// handler without constructing a network transport, socket, or listener. The
// supplied handler must enforce the application's read-only method boundary.
func NewInProcess(handler http.Handler) Source {
	return &Remote{endpoint: "https://offline.invalid", http: &http.Client{Transport: handlerTransport{handler}, Timeout: 45 * time.Second}}
}

type handlerTransport struct{ handler http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	w := &memoryResponse{header: make(http.Header)}
	t.handler.ServeHTTP(w, r)
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	if w.overflow {
		return nil, errors.New("response exceeds 1 MiB; narrow the query")
	}
	code := w.code
	if code == 0 {
		code = http.StatusOK
	}
	return &http.Response{StatusCode: code, Header: w.header, Body: io.NopCloser(bytes.NewReader(w.body.Bytes())), Request: r}, nil
}

// ASVS 2.2.1: enforce the response budget while writing, before allocating an
// unbounded recorder; Connect additionally enforces decompressed message size.
type memoryResponse struct {
	header   http.Header
	body     bytes.Buffer
	code     int
	overflow bool
}

func (w *memoryResponse) Header() http.Header { return w.header }
func (w *memoryResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *memoryResponse) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	if w.body.Len()+len(p) > MaxResponseBytes {
		w.overflow = true
		return 0, errors.New("response exceeds budget")
	}
	return w.body.Write(p)
}
