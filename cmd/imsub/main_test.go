package main

import (
	"errors"
	"io"
	"testing"
)

func TestRunWithoutAdminRunsServer(t *testing.T) {
	oldRunServer := runServer
	oldRunAdminCommand := runAdminCommand
	defer func() {
		runServer = oldRunServer
		runAdminCommand = oldRunAdminCommand
	}()

	serverCalled := false
	adminCalled := false
	runServer = func() error {
		serverCalled = true
		return nil
	}
	runAdminCommand = func([]string, io.Writer) error {
		adminCalled = true
		return nil
	}

	if err := run(nil); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !serverCalled {
		t.Fatal("runServer was not called")
	}
	if adminCalled {
		t.Fatal("runAdminCommand called for normal server run")
	}
}

func TestRunAdminDispatchesAdminArgs(t *testing.T) {
	oldRunServer := runServer
	oldRunAdminCommand := runAdminCommand
	defer func() {
		runServer = oldRunServer
		runAdminCommand = oldRunAdminCommand
	}()

	serverCalled := false
	var gotArgs []string
	runServer = func() error {
		serverCalled = true
		return nil
	}
	runAdminCommand = func(args []string, _ io.Writer) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	if err := run([]string{"admin", "member-tags-refresh", "-chat-id", "-100"}); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if serverCalled {
		t.Fatal("runServer called for admin command")
	}
	want := []string{"member-tags-refresh", "-chat-id", "-100"}
	if len(gotArgs) != len(want) {
		t.Fatalf("admin args = %v, want %v", gotArgs, want)
	}
	for i := range want {
		if gotArgs[i] != want[i] {
			t.Fatalf("admin args = %v, want %v", gotArgs, want)
		}
	}
}

func TestRunReturnsAdminError(t *testing.T) {
	oldRunServer := runServer
	oldRunAdminCommand := runAdminCommand
	defer func() {
		runServer = oldRunServer
		runAdminCommand = oldRunAdminCommand
	}()

	wantErr := errors.New("admin failed")
	runServer = func() error { return nil }
	runAdminCommand = func([]string, io.Writer) error { return wantErr }

	if err := run([]string{"admin"}); !errors.Is(err, wantErr) {
		t.Fatalf("run() error = %v, want %v", err, wantErr)
	}
}
