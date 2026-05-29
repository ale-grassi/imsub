package redis

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"imsub/internal/events"
)

type redisCommandObservation struct {
	job     string
	command string
	result  string
	count   int
}

type redisCommandObserverStub struct {
	observed []redisCommandObservation
}

func (s *redisCommandObserverStub) ObserveRedisCommand(_ context.Context, job, command, result string, count int) {
	s.observed = append(s.observed, redisCommandObservation{job: job, command: command, result: result, count: count})
}

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
	if _, ok := got[keyBackupDirty()]; ok {
		t.Fatal("backup unexpectedly included backup metadata key")
	}
}

func TestExportBackupSkipsTemporaryDumpKeys(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	subscriberTmp := s.NewSubscriberDumpKey("creator-1")
	if err := s.AddToSubscriberDump(ctx, subscriberTmp, []string{"sub-1"}); err != nil {
		t.Fatalf("AddToSubscriberDump() error = %v", err)
	}
	blocklistTmp := s.NewCreatorBlocklistDumpKey("creator-1")
	if err := s.AddToCreatorBlocklistDump(ctx, blocklistTmp, []string{"blocked-1"}); err != nil {
		t.Fatalf("AddToCreatorBlocklistDump() error = %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:plain", "value", 0).Err(); err != nil {
		t.Fatalf("seed plain key: %v", err)
	}

	var buf bytes.Buffer
	count, err := s.ExportBackup(ctx, &buf)
	if err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("ExportBackup() count = %d, want 1", count)
	}
	entries := decodeBackupEntries(t, &buf)
	if len(entries) != 1 || entries[0].Key != "imsub:plain" {
		t.Fatalf("entries = %+v, want only imsub:plain", entries)
	}
}

func TestCreateIncrementalBackupWritesDirtyKeysAndTombstones(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	if err := s.rdb.Set(ctx, "imsub:dirty", "value", 0).Err(); err != nil {
		t.Fatalf("seed dirty key: %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:deleted", "gone", 0).Err(); err != nil {
		t.Fatalf("seed deleted key: %v", err)
	}
	if err := s.rdb.Del(ctx, "imsub:deleted").Err(); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:backup:internal", "skip", 0).Err(); err != nil {
		t.Fatalf("seed backup metadata: %v", err)
	}
	if err := s.rdb.Set(skipBackupTracking(ctx), keyBackupBaseFullKey(), "backups/full/base.jsonl.gz", 0).Err(); err != nil {
		t.Fatalf("seed base full key: %v", err)
	}

	var buf bytes.Buffer
	count, token, err := s.CreateBackup(ctx, &buf, BackupKindIncremental, "backups/incremental/next.jsonl.gz", time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if token == "" {
		t.Fatal("CreateBackup() token is empty")
	}
	if count != 2 {
		t.Fatalf("CreateBackup() count = %d, want 2", count)
	}
	entries := decodeBackupEntries(t, &buf)
	gotTypes := map[string]string{}
	for _, entry := range entries {
		gotTypes[entry.Key] = entry.Type
	}
	if gotTypes["imsub:dirty"] != "key" {
		t.Fatalf("dirty record type = %q, want key", gotTypes["imsub:dirty"])
	}
	if gotTypes["imsub:deleted"] != "delete" {
		t.Fatalf("deleted record type = %q, want delete", gotTypes["imsub:deleted"])
	}
	if _, ok := gotTypes["imsub:backup:internal"]; ok {
		t.Fatal("incremental unexpectedly included backup metadata key")
	}
}

func TestTemporaryDumpWritesDoNotEnterBackupTracking(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()

	subscriberTmp := s.NewSubscriberDumpKey("creator-1")
	if err := s.AddToSubscriberDump(ctx, subscriberTmp, []string{"sub-1", "sub-2"}); err != nil {
		t.Fatalf("AddToSubscriberDump() error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), subscriberTmp).Result(); err != nil || ok {
		t.Fatalf("subscriber temp dirty member = (%t, %v), want false nil", ok, err)
	}
	if err := s.FinalizeSubscriberDump(ctx, "creator-1", subscriberTmp, true); err != nil {
		t.Fatalf("FinalizeSubscriberDump() error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), keyCreatorSubscribers("creator-1")).Result(); err != nil || !ok {
		t.Fatalf("subscriber dest dirty member = (%t, %v), want true nil", ok, err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), subscriberTmp).Result(); err != nil || ok {
		t.Fatalf("subscriber temp deleted member = (%t, %v), want false nil", ok, err)
	}

	blocklistTmp := s.NewCreatorBlocklistDumpKey("creator-1")
	if err := s.AddToCreatorBlocklistDump(ctx, blocklistTmp, []string{"blocked-1"}); err != nil {
		t.Fatalf("AddToCreatorBlocklistDump() error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), blocklistTmp).Result(); err != nil || ok {
		t.Fatalf("blocklist temp dirty member = (%t, %v), want false nil", ok, err)
	}
	if err := s.FinalizeCreatorBlocklistDump(ctx, "creator-1", blocklistTmp, true); err != nil {
		t.Fatalf("FinalizeCreatorBlocklistDump() error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), keyCreatorBlockedUsers("creator-1")).Result(); err != nil || !ok {
		t.Fatalf("blocklist dest dirty member = (%t, %v), want true nil", ok, err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), blocklistTmp).Result(); err != nil || ok {
		t.Fatalf("blocklist temp deleted member = (%t, %v), want false nil", ok, err)
	}

	if err := s.FinalizeSubscriberDump(ctx, "creator-empty", "", false); err != nil {
		t.Fatalf("FinalizeSubscriberDump(empty) error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), keyCreatorSubscribers("creator-empty")).Result(); err != nil || !ok {
		t.Fatalf("empty subscriber dest deleted member = (%t, %v), want true nil", ok, err)
	}
	if err := s.FinalizeCreatorBlocklistDump(ctx, "creator-empty", "", false); err != nil {
		t.Fatalf("FinalizeCreatorBlocklistDump(empty) error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), keyCreatorBlockedUsers("creator-empty")).Result(); err != nil || !ok {
		t.Fatalf("empty blocklist dest deleted member = (%t, %v), want true nil", ok, err)
	}
}

func TestCreateIncrementalBackupSkipsTombstoneForRecreatedKey(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	if err := s.rdb.Set(ctx, "imsub:recreated", "old", 0).Err(); err != nil {
		t.Fatalf("seed key: %v", err)
	}
	if err := s.rdb.Del(skipBackupTracking(ctx), keyBackupDirty(), keyBackupDeleted()).Err(); err != nil {
		t.Fatalf("clear tracking sets: %v", err)
	}
	if err := s.rdb.Set(skipBackupTracking(ctx), keyBackupBaseFullKey(), "backups/full/base.jsonl.gz", 0).Err(); err != nil {
		t.Fatalf("seed base full key: %v", err)
	}
	if err := s.rdb.Del(ctx, "imsub:recreated").Err(); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:recreated", "new", 0).Err(); err != nil {
		t.Fatalf("recreate key: %v", err)
	}

	var buf bytes.Buffer
	count, _, err := s.CreateBackup(ctx, &buf, BackupKindIncremental, "backups/incremental/next.jsonl.gz", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("CreateBackup() count = %d, want 1", count)
	}
	entries := decodeBackupEntries(t, &buf)
	if len(entries) != 1 || entries[0].Key != "imsub:recreated" || entries[0].Type != "key" {
		t.Fatalf("entries = %+v, want one key record for recreated key", entries)
	}
}

func TestBackupTrackingHookRecordsMutationCommands(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	if err := s.rdb.HSet(ctx, "imsub:hash", "field", "value").Err(); err != nil {
		t.Fatalf("HSet() error = %v", err)
	}
	if err := s.rdb.SAdd(ctx, "imsub:set", "one").Err(); err != nil {
		t.Fatalf("SAdd() error = %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:rename:src", "value", 0).Err(); err != nil {
		t.Fatalf("Set(rename src) error = %v", err)
	}
	if err := s.rdb.Rename(ctx, "imsub:rename:src", "imsub:rename:dst").Err(); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	if err := s.rdb.Expire(ctx, "imsub:hash", time.Minute).Err(); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
	if err := s.rdb.Del(ctx, "imsub:set").Err(); err != nil {
		t.Fatalf("Del() error = %v", err)
	}

	for _, key := range []string{"imsub:hash", "imsub:set", "imsub:rename:src", "imsub:rename:dst"} {
		if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), key).Result(); err != nil || !ok {
			t.Fatalf("dirty member %q = (%t, %v), want true nil", key, ok, err)
		}
	}
	for _, key := range []string{"imsub:set", "imsub:rename:src"} {
		if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDeleted(), key).Result(); err != nil || !ok {
			t.Fatalf("deleted member %q = (%t, %v), want true nil", key, ok, err)
		}
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:backup:dirty").Result(); err != nil || ok {
		t.Fatalf("backup metadata dirty member = (%t, %v), want false nil", ok, err)
	}
}

func TestRedisCommandMetricsHookRecordsJobContext(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	observer := &redisCommandObserverStub{}
	s.SetCommandObserver(observer)
	ctx := events.WithBackgroundJobContext(t.Context(), "sync_member_tags", "run-1")

	if err := s.rdb.Set(ctx, "imsub:metric", "value", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	pipe := s.rdb.Pipeline()
	pipe.Get(ctx, "imsub:metric")
	pipe.Get(ctx, "imsub:metric")
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	want := map[redisCommandObservation]int{
		{job: "sync_member_tags", command: "set", result: "ok", count: 1}:            1,
		{job: "sync_member_tags", command: "get", result: "ok", count: 1}:            2,
		{job: "sync_member_tags", command: redisCommandSAdd, result: "ok", count: 1}: 1,
	}
	for _, got := range observer.observed {
		want[got]--
	}
	for observation, remaining := range want {
		if remaining > 0 {
			t.Fatalf("missing observation %+v in %+v", observation, observer.observed)
		}
	}
}

func TestRedisCommandMetricsHookRecordsForegroundOperationContext(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	observer := &redisCommandObserverStub{}
	s.SetCommandObserver(observer)
	ctx := events.WithForegroundOperationContext(t.Context(), "telegram_command_start")

	if err := s.rdb.Set(ctx, "imsub:foreground_metric", "value", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	for _, got := range observer.observed {
		if got.job == "telegram_command_start" && got.command == "set" && got.result == "ok" {
			return
		}
	}
	t.Fatalf("missing foreground operation observation in %+v", observer.observed)
}

func TestRedisCommandMetricsHookPrefersBackgroundJobContext(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	observer := &redisCommandObserverStub{}
	s.SetCommandObserver(observer)
	ctx := events.WithForegroundOperationContext(t.Context(), "telegram_command_start")
	ctx = events.WithBackgroundJobContext(ctx, "sync_member_tags", "run-1")

	if err := s.rdb.Set(ctx, "imsub:background_metric", "value", 0).Err(); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	for _, got := range observer.observed {
		if got.command != "set" || got.result != "ok" {
			continue
		}
		if got.job == "sync_member_tags" {
			return
		}
		t.Fatalf("Set() job = %q, want sync_member_tags in %+v", got.job, observer.observed)
	}
	t.Fatalf("missing background job observation in %+v", observer.observed)
}

func TestBackupTrackingHookAggregatesPipelineTrackingCommands(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	observer := &redisCommandObserverStub{}
	s.SetCommandObserver(observer)
	ctx := t.Context()

	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, "imsub:hash", "field", "value")
	pipe.SAdd(ctx, "imsub:set", "one")
	if _, err := pipe.Exec(ctx); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}

	saddCount := 0
	for _, got := range observer.observed {
		if got.command == redisCommandSAdd && got.result == "ok" {
			saddCount += got.count
		}
	}
	if saddCount != 2 {
		t.Fatalf("observed successful SADD count = %d in %+v, want 2 (one app command plus one aggregated backup tracker command)", saddCount, observer.observed)
	}
}

func TestFinishBackupUploadFailureRequeuesRotatedSets(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	if err := s.rdb.Set(ctx, "imsub:dirty", "value", 0).Err(); err != nil {
		t.Fatalf("seed dirty key: %v", err)
	}

	var buf bytes.Buffer
	_, token, err := s.CreateBackup(ctx, &buf, BackupKindIncremental, "backups/incremental/next.jsonl.gz", time.Now().UTC())
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if err := s.FinishBackup(ctx, token, false, BackupKindIncremental, "backups/incremental/next.jsonl.gz", time.Now().UTC()); err != nil {
		t.Fatalf("FinishBackup(false) error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:dirty").Result(); err != nil || !ok {
		t.Fatalf("dirty key requeued = (%t, %v), want true nil", ok, err)
	}
}

func TestRestoreBackupRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	var wrongFormat bytes.Buffer
	gz := gzip.NewWriter(&wrongFormat)
	if err := json.NewEncoder(gz).Encode(backupManifest{Format: "wrong", Kind: BackupKindFull}); err != nil {
		t.Fatalf("encode wrong manifest: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close wrong format gzip: %v", err)
	}
	if _, err := s.RestoreBackup(ctx, bytes.NewReader(wrongFormat.Bytes())); err == nil {
		t.Fatal("RestoreBackup(wrong format) error = nil, want non-nil")
	}

	var badEntry bytes.Buffer
	gz = gzip.NewWriter(&badEntry)
	enc := json.NewEncoder(gz)
	if err := enc.Encode(backupManifest{Format: backupFormat, Kind: BackupKindIncremental}); err != nil {
		t.Fatalf("encode manifest: %v", err)
	}
	if err := enc.Encode(BackupEntry{Type: "unknown", Key: "imsub:key"}); err != nil {
		t.Fatalf("encode bad entry: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close bad entry gzip: %v", err)
	}
	if _, err := s.RestoreBackup(ctx, bytes.NewReader(badEntry.Bytes())); err == nil {
		t.Fatal("RestoreBackup(bad entry) error = nil, want non-nil")
	}
}

func TestFinishBackupUploadSuccessClearsOnlyRotatedSets(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ctx := t.Context()
	if err := s.rdb.Set(ctx, "imsub:before", "value", 0).Err(); err != nil {
		t.Fatalf("seed before key: %v", err)
	}
	var buf bytes.Buffer
	now := time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC)
	_, token, err := s.CreateBackup(ctx, &buf, BackupKindFull, "backups/full/next.jsonl.gz", now)
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if err := s.rdb.Set(ctx, "imsub:after", "value", 0).Err(); err != nil {
		t.Fatalf("seed after key: %v", err)
	}
	if err := s.FinishBackup(ctx, token, true, BackupKindFull, "backups/full/next.jsonl.gz", now); err != nil {
		t.Fatalf("FinishBackup(true) error = %v", err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:before").Result(); err != nil || ok {
		t.Fatalf("rotated dirty key present = (%t, %v), want false nil", ok, err)
	}
	if ok, err := s.rdb.SIsMember(skipBackupTracking(ctx), keyBackupDirty(), "imsub:after").Result(); err != nil || !ok {
		t.Fatalf("live dirty key present = (%t, %v), want true nil", ok, err)
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

func TestRestoreBackupAppliesFullThenIncrementalWithDelete(t *testing.T) {
	t.Parallel()

	source := newTestStore(t)
	ctx := t.Context()
	if err := source.rdb.Set(ctx, "imsub:keep", "old", 0).Err(); err != nil {
		t.Fatalf("seed keep: %v", err)
	}
	if err := source.rdb.Set(ctx, "imsub:remove", "present", 0).Err(); err != nil {
		t.Fatalf("seed remove: %v", err)
	}

	var full bytes.Buffer
	if _, err := source.ExportBackup(ctx, &full); err != nil {
		t.Fatalf("ExportBackup() error = %v", err)
	}
	if err := source.rdb.Del(skipBackupTracking(ctx), keyBackupDirty(), keyBackupDeleted()).Err(); err != nil {
		t.Fatalf("clear tracking sets: %v", err)
	}
	if err := source.rdb.Set(skipBackupTracking(ctx), keyBackupBaseFullKey(), "backups/full/base.jsonl.gz", 0).Err(); err != nil {
		t.Fatalf("seed base key: %v", err)
	}
	if err := source.rdb.Set(ctx, "imsub:keep", "new", 0).Err(); err != nil {
		t.Fatalf("update keep: %v", err)
	}
	if err := source.rdb.Del(ctx, "imsub:remove").Err(); err != nil {
		t.Fatalf("delete remove: %v", err)
	}
	var incremental bytes.Buffer
	if _, _, err := source.CreateBackup(ctx, &incremental, BackupKindIncremental, "backups/incremental/next.jsonl.gz", time.Now().UTC()); err != nil {
		t.Fatalf("CreateBackup(incremental) error = %v", err)
	}

	target := newTestStore(t)
	if _, err := target.RestoreBackup(ctx, bytes.NewReader(full.Bytes())); err != nil {
		t.Fatalf("RestoreBackup(full) error = %v", err)
	}
	if _, err := target.RestoreBackup(ctx, bytes.NewReader(incremental.Bytes())); err != nil {
		t.Fatalf("RestoreBackup(incremental) error = %v", err)
	}
	if got, err := target.rdb.Get(ctx, "imsub:keep").Result(); err != nil || got != "new" {
		t.Fatalf("restored imsub:keep = (%q, %v), want new nil", got, err)
	}
	if exists, err := target.rdb.Exists(ctx, "imsub:remove").Result(); err != nil || exists != 0 {
		t.Fatalf("restored imsub:remove exists = (%d, %v), want 0 nil", exists, err)
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

func TestBackupTTLMillis(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ttl    time.Duration
		wantMS int64
		wantOK bool
	}{
		{name: "persistent", ttl: -1, wantMS: -1, wantOK: true},
		{name: "missing", ttl: -2, wantMS: 0, wantOK: false},
		{name: "positive ttl", ttl: 25 * time.Millisecond, wantMS: 25, wantOK: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotMS, gotOK := backupTTLMillis(tt.ttl)
			if gotMS != tt.wantMS || gotOK != tt.wantOK {
				t.Fatalf("backupTTLMillis(%v) = (%d, %t), want (%d, %t)", tt.ttl, gotMS, gotOK, tt.wantMS, tt.wantOK)
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
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("decode backup entry: %v", err)
		}
		var manifest backupManifest
		if err := json.Unmarshal(raw, &manifest); err == nil && manifest.Format == backupFormat {
			continue
		}
		var entry BackupEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatalf("decode backup entry: %v", err)
		}
		entries = append(entries, entry)
	}
	return entries
}
