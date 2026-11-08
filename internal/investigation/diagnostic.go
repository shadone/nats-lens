package investigation

// Diagnostic is a bounded, path-free machine-readable investigation failure.
type Diagnostic struct{ Code, Message string }

func (d *Diagnostic) Error() string { return d.Message }
