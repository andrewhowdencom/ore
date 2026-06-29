package junk

import "errors"

// ErrThreadNotFound is returned by Store.Get and Store.GetBy when no
// thread matches the requested identifier (UUID for Get, metadata
// key-value pair for GetBy). Callers can distinguish this from a
// parse failure with errors.Is.
var ErrThreadNotFound = errors.New("thread not found")

// ErrThreadCorrupt is the sentinel wrapped by Store.Get and
// Store.GetBy when a thread file exists but cannot be parsed. The
// underlying error is the actual unmarshal failure; callers can
// recover it via errors.Unwrap or errors.Is.
//
// Use errors.Is(err, ErrThreadCorrupt) to detect this case. The
// wrapped error itself is constructed with fmt.Errorf("...: %w: %w",
// ErrThreadCorrupt, underlyingErr) so that errors.Is succeeds against
// either target.
var ErrThreadCorrupt = errors.New("thread file corrupt")
