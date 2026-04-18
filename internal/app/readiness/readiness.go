// Package readiness exposes a process-local readiness flag used to gate /healthz
// until the bot has finished its remote startup side-effects.
package readiness

import "sync/atomic"

// Flag is a process-local boolean set to true when the bot is ready to serve.
// The zero value is a valid, not-yet-ready Flag.
type Flag struct {
	ready atomic.Bool
}

// New returns a Flag initialized to not-ready.
func New() *Flag { return &Flag{} }

// MarkReady transitions the flag to ready. Idempotent.
func (f *Flag) MarkReady() {
	if f == nil {
		return
	}
	f.ready.Store(true)
}

// Ready reports whether the app has completed startup.
// A nil receiver is treated as not ready.
func (f *Flag) Ready() bool {
	if f == nil {
		return false
	}
	return f.ready.Load()
}
