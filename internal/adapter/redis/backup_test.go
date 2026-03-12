package redis

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestExportBackupWritesGzipJSONLinesForImsubKeys(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.rdb.Set(ctx, "imsub:plain", "value-1", 0).Err(); err != nil {
		t.Fatalf("seed plain key: %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:ttl", "value-2", 2*time.Minute).Err(); err != nil {
		t.Fatalf("seed ttl key: %v", err)
	}
	if err := s.rdb.Set(ctx, "other:key", "skip-me", 0).Err(); err != nil {
		t.Fatalf("seed foreign key: %v", err)
	}

	var buf bytes.Buffer
	count, err := s.ExportBackup(ctx, &buf)
	if err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("ExportBackup() count = %d, want 2", count)
	}

	entries := decodeBackupEntries(t, &buf)
	if len(entries) != 2 {
		t.Fatalf("backup entry count = %d, want 2", len(entries))
	}
	got := map[string]BackupEntry{}
	for _, entry := range entries {
		got[entry.Key] = entry
	}
	if got["imsub:plain"].TTLMS > 0 {
		t.Fatalf("plain ttl_ms = %d, want non-positive for a persistent key", got["imsub:plain"].TTLMS)
	}
	if got["imsub:ttl"].TTLMS <= 0 {
		t.Fatalf("ttl ttl_ms = %d, want > 0", got["imsub:ttl"].TTLMS)
	}
	if _, ok := got["other:key"]; ok {
		t.Fatal("backup unexpectedly included non-imsub key")
	}
}

func TestRestoreBackupRestoresStringKeysAndTTL(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	if err := s.rdb.Set(ctx, "imsub:source", "payload", 0).Err(); err != nil {
		t.Fatalf("seed source key: %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:ttl", "ephemeral", 90*time.Second).Err(); err != nil {
		t.Fatalf("seed ttl source key: %v", err)
	}

	var backup bytes.Buffer
	if _, err := s.ExportBackup(ctx, &backup); err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	if err := s.rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("FlushDB() error = %v", err)
	}

	count, err := s.RestoreBackup(ctx, bytes.NewReader(backup.Bytes()))
	if err != nil {
		t.Fatalf("RestoreBackup() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("RestoreBackup() count = %d, want 2", count)
	}
	if got, err := s.rdb.Get(ctx, "imsub:source").Result(); err != nil || got != "payload" {
		t.Fatalf("restored imsub:source = (%q, %v), want (%q, nil)", got, err, "payload")
	}
	if got, err := s.rdb.Get(ctx, "imsub:ttl").Result(); err != nil || got != "ephemeral" {
		t.Fatalf("restored imsub:ttl = (%q, %v), want (%q, nil)", got, err, "ephemeral")
	}
	if ttl, err := s.rdb.PTTL(ctx, "imsub:ttl").Result(); err != nil || ttl <= 0 {
		t.Fatalf("restored ttl = (%v, %v), want > 0", ttl, err)
	}
}

func TestRestoreTTL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ttlMS   int64
		want    time.Duration
		wantErr bool
	}{
		{name: "persistent", ttlMS: -1, want: 0},
		{name: "minimum expiring", ttlMS: 0, want: time.Millisecond},
		{name: "positive ttl", ttlMS: 25, want: 25 * time.Millisecond},
		{name: "invalid", ttlMS: -2, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := restoreTTL(tt.ttlMS)
			if tt.wantErr {
				if err == nil {
					t.Fatal("restoreTTL() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("restoreTTL() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Fatalf("restoreTTL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func decodeBackupEntries(t *testing.T, r io.Reader) []BackupEntry {
	t.Helper()

	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer gz.Close()

	dec := json.NewDecoder(gz)
	var entries []BackupEntry
	for {
		var entry BackupEntry
		if err := dec.Decode(&entry); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode backup entry: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}
