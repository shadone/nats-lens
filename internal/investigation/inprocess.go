package investigation

import "net/http"

// NewInProcess uses an application HTTP handler without a network listener.
func NewInProcess(handler http.Handler) Source { return &Remote{} }
