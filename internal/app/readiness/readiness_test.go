package readiness

import "testing"

func TestFlagLifecycle(t *testing.T) {
	t.Parallel()
	f := New()
	if f.Ready() {
		t.Fatal("new Flag.Ready() = true, want false")
	}
	f.MarkReady()
	if !f.Ready() {
		t.Fatal("after MarkReady, Flag.Ready() = false, want true")
	}
	f.MarkReady()
	if !f.Ready() {
		t.Fatal("MarkReady is not idempotent")
	}
}

func TestNilFlagNotReady(t *testing.T) {
	t.Parallel()
	var f *Flag
	if f.Ready() {
		t.Fatal("nil Flag.Ready() = true, want false")
	}
	f.MarkReady()
	if f.Ready() {
		t.Fatal("nil Flag.MarkReady should not panic or flip")
	}
}
