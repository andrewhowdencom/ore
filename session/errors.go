package session

import "errors"

// errSessionClosed is the sentinel returned by Session.Run, workQueue.submit,
// and similar entry points when the session has been closed.
var errSessionClosed = errors.New("session is closed")