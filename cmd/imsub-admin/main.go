package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"imsub/internal/adapter/redis"
	"imsub/internal/adapter/s3"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/joho/godotenv"
)

const backupLoadConfirmValue = "backup-load"
const backupFormat = "imsub-redis-backup"

var (
	errBackupLoadConfirmRequired = errors.New("backup load requires explicit confirmation")
	errRedisConfigMissing        = errors.New("missing redis env vars")
	errBackupConfigMissing       = errors.New("missing backup env vars")
	errNoBackupObjects           = errors.New("no backup objects found under backups/")
	errBackupBaseMissing         = errors.New("backup base is missing")
	errDownloadOutRequired       = errors.New("download requires -out")
	errMTProtoConfigMissing      = errors.New("missing mtproto env vars")
	errInvalidMTProtoAppID       = errors.New("invalid IMSUB_TELEGRAM_MTPROTO_API_ID")
	errMTProtoSignupUnsupported  = errors.New("telegram sign-up is not supported for mtproto-session")
	errUsage                     = errors.New("usage")
)

type backupConfig struct {
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Region          string
}

type backupManifest struct {
	Format      string `json:"format"`
	Kind        string `json:"kind"`
	CreatedAt   string `json:"created_at"`
	BaseFullKey string `json:"base_full_key,omitempty"`
}

type mtprotoConfig struct {
	AppID   int
	AppHash string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("imsub-admin failed: %v", err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError("missing command")
	}

	switch args[0] {
	case "backup-download":
		return runDownload(args[1:])
	case "backup-load":
		return runBackupLoad(args[1:])
	case "mtproto-session":
		return runMTProtoSession(args[1:])
	default:
		return usageError("unknown command %q", args[0])
	}
}

func runMTProtoSession(args []string) error {
	fs := flag.NewFlagSet("mtproto-session", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	envPath := fs.String("env", ".env.dev", "path to .env file")
	phone := fs.String("phone", "", "phone number for the Telegram user account")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse mtproto-session flags: %w", err)
	}

	cfg, err := loadMTProtoConfig(*envPath)
	if err != nil {
		return err
	}

	storage := &session.StorageMemory{}
	client := telegram.NewClient(cfg.AppID, cfg.AppHash, telegram.Options{
		SessionStorage: storage,
	})
	prompter := &interactiveAuth{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
		phone:  strings.TrimSpace(*phone),
	}

	if err := client.Run(context.Background(), func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("load auth status: %w", err)
		}
		if status.Authorized {
			return nil
		}
		return auth.NewFlow(prompter, auth.SendCodeOptions{}).Run(ctx, client.Auth())
	}); err != nil {
		return fmt.Errorf("generate mtproto session: %w", err)
	}

	raw, err := storage.Bytes(nil)
	if err != nil {
		return fmt.Errorf("read mtproto session: %w", err)
	}
	if _, err := fmt.Fprintf(os.Stdout, "IMSUB_TELEGRAM_MTPROTO_SESSION=%s\n", base64.StdEncoding.EncodeToString(raw)); err != nil {
		return fmt.Errorf("write mtproto session: %w", err)
	}
	return nil
}

func runDownload(args []string) error {
	fs := flag.NewFlagSet("backup-download", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	envPath := fs.String("env", ".env", "path to .env file")
	key := fs.String("key", "", "backup object key to download")
	outPath := fs.String("out", "", "local output path for the downloaded backup")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse download flags: %w", err)
	}
	if strings.TrimSpace(*outPath) == "" {
		return errDownloadOutRequired
	}
	targetPath := cleanOutputPath(*outPath)

	cfg, err := loadBackupConfig(*envPath)
	if err != nil {
		return err
	}
	s3Client, err := s3.NewClient(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3Region)
	if err != nil {
		return fmt.Errorf("new s3 client: %w", err)
	}

	ctx := context.Background()
	objectKey, err := resolveBackupObjectKey(ctx, s3Client, *key)
	if err != nil {
		return err
	}

	rc, err := s3Client.Download(ctx, objectKey)
	if err != nil {
		return fmt.Errorf("download backup object %q: %w", objectKey, err)
	}
	defer func() { _ = rc.Close() }()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return fmt.Errorf("create output directory for %s: %w", targetPath, err)
	}
	// #nosec G304 -- targetPath comes from the operator-provided -out flag and is cleaned before use.
	f, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("create output file %s: %w", targetPath, err)
	}
	defer func() { _ = f.Close() }()

	if _, err := io.Copy(f, rc); err != nil {
		return fmt.Errorf("write output file %s: %w", targetPath, err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "backup_key=%s output=%s download_completed=true\n", objectKey, targetPath); err != nil {
		return fmt.Errorf("write download summary: %w", err)
	}
	return nil
}

func runBackupLoad(args []string) error {
	fs := flag.NewFlagSet("backup-load", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	envPath := fs.String("env", ".env", "path to .env file")
	key := fs.String("key", "", "backup object key to load")
	fromFile := fs.String("from-file", "", "local backup file to load instead of downloading from object storage")
	redisURL := fs.String("redis-url", "", "override IMSUB_REDIS_URL from env file")
	confirm := fs.String("confirm", "", "required confirmation value")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse backup-load flags: %w", err)
	}
	if strings.TrimSpace(*confirm) != backupLoadConfirmValue {
		return fmt.Errorf("%w: -confirm=%s", errBackupLoadConfirmRequired, backupLoadConfirmValue)
	}

	effectiveRedisURL, err := resolveRedisURL(*envPath, *redisURL)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store, err := redis.NewStore(effectiveRedisURL, logger)
	if err != nil {
		return fmt.Errorf("new redis store: %w", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if strings.TrimSpace(*fromFile) != "" {
		rc, sourceLabel, err := openBackupSource(ctx, *envPath, *key, *fromFile)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		if _, err := store.RestoreBackup(ctx, rc); err != nil {
			return fmt.Errorf("load backup %q: %w", sourceLabel, err)
		}
	} else if err := restoreBackupFromObjectStorage(ctx, store, *envPath, *key); err != nil {
		return err
	}

	if _, err := io.WriteString(os.Stdout, "backup_load_completed=true\n"); err != nil {
		return fmt.Errorf("write backup load summary: %w", err)
	}
	return nil
}

func restoreBackupFromObjectStorage(ctx context.Context, store *redis.Store, envPath, key string) error {
	cfg, err := loadBackupConfig(envPath)
	if err != nil {
		return err
	}
	s3Client, err := s3.NewClient(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3Region)
	if err != nil {
		return fmt.Errorf("new s3 client: %w", err)
	}
	selectedKey, err := resolveBackupObjectKey(ctx, s3Client, key)
	if err != nil {
		return err
	}
	chain, err := resolveRestoreChain(ctx, s3Client, selectedKey)
	if err != nil {
		return err
	}
	for _, objectKey := range chain {
		rc, err := s3Client.Download(ctx, objectKey)
		if err != nil {
			return fmt.Errorf("download backup object %q: %w", objectKey, err)
		}
		_, restoreErr := store.RestoreBackup(ctx, rc)
		closeErr := rc.Close()
		if restoreErr != nil {
			return fmt.Errorf("load backup %q: %w", objectKey, restoreErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close backup object %q: %w", objectKey, closeErr)
		}
	}
	return nil
}

func resolveRestoreChain(ctx context.Context, store backupObjectStore, selectedKey string) ([]string, error) {
	selectedBytes, err := downloadBackupBytes(ctx, store, selectedKey)
	if err != nil {
		return nil, err
	}
	manifest, ok, err := inspectBackupManifest(selectedBytes)
	if err != nil {
		return nil, fmt.Errorf("inspect backup %q: %w", selectedKey, err)
	}
	if !ok || manifest.Kind != "incremental" {
		return []string{selectedKey}, nil
	}
	if manifest.BaseFullKey == "" {
		return nil, fmt.Errorf("%w for incremental backup %q", errBackupBaseMissing, selectedKey)
	}
	objects, err := store.ListPrefix(ctx, "backups/")
	if err != nil {
		return nil, fmt.Errorf("list backup objects: %w", err)
	}
	objectByKey := map[string]s3.ObjectInfo{}
	for _, object := range objects {
		objectByKey[object.Key] = object
	}
	selectedObject, ok := objectByKey[selectedKey]
	if !ok {
		selectedObject = s3.ObjectInfo{Key: selectedKey}
	}
	baseObject, ok := objectByKey[manifest.BaseFullKey]
	if !ok {
		return nil, fmt.Errorf("%w: %q for %q", errBackupBaseMissing, manifest.BaseFullKey, selectedKey)
	}
	chain := []s3.ObjectInfo{baseObject}
	for _, object := range objects {
		if object.Key == manifest.BaseFullKey || object.Key == selectedKey || !strings.Contains(object.Key, "/incremental/") {
			continue
		}
		if !object.LastModified.After(baseObject.LastModified) || object.LastModified.After(selectedObject.LastModified) {
			continue
		}
		raw, err := downloadBackupBytes(ctx, store, object.Key)
		if err != nil {
			return nil, err
		}
		candidateManifest, ok, err := inspectBackupManifest(raw)
		if err != nil {
			return nil, fmt.Errorf("inspect backup %q: %w", object.Key, err)
		}
		if ok && candidateManifest.Kind == "incremental" && candidateManifest.BaseFullKey == manifest.BaseFullKey {
			chain = append(chain, object)
		}
	}
	chain = append(chain, selectedObject)
	sort.Slice(chain, func(i, j int) bool {
		if chain[i].LastModified.Equal(chain[j].LastModified) {
			return chain[i].Key < chain[j].Key
		}
		return chain[i].LastModified.Before(chain[j].LastModified)
	})
	out := make([]string, 0, len(chain))
	for _, object := range chain {
		if len(out) == 0 || out[len(out)-1] != object.Key {
			out = append(out, object.Key)
		}
	}
	return out, nil
}

func downloadBackupBytes(ctx context.Context, store backupObjectStore, key string) ([]byte, error) {
	rc, err := store.Download(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("download backup object %q: %w", key, err)
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read backup object %q: %w", key, err)
	}
	return raw, nil
}

func inspectBackupManifest(raw []byte) (backupManifest, bool, error) {
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return backupManifest{}, false, fmt.Errorf("open backup gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()
	dec := json.NewDecoder(gz)
	var manifest backupManifest
	if err := dec.Decode(&manifest); err != nil {
		if errors.Is(err, io.EOF) {
			return backupManifest{}, false, nil
		}
		return backupManifest{}, false, fmt.Errorf("decode first backup record: %w", err)
	}
	if manifest.Format != backupFormat {
		return backupManifest{}, false, nil
	}
	return manifest, true, nil
}

func openBackupSource(ctx context.Context, envPath, key, fromFile string) (io.ReadCloser, string, error) {
	if strings.TrimSpace(fromFile) != "" {
		f, err := os.Open(strings.TrimSpace(fromFile))
		if err != nil {
			return nil, "", fmt.Errorf("open backup file %s: %w", strings.TrimSpace(fromFile), err)
		}
		return f, strings.TrimSpace(fromFile), nil
	}

	cfg, err := loadBackupConfig(envPath)
	if err != nil {
		return nil, "", err
	}
	s3Client, err := s3.NewClient(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3Region)
	if err != nil {
		return nil, "", fmt.Errorf("new s3 client: %w", err)
	}
	objectKey, err := resolveBackupObjectKey(ctx, s3Client, key)
	if err != nil {
		return nil, "", err
	}
	rc, err := s3Client.Download(ctx, objectKey)
	if err != nil {
		return nil, "", fmt.Errorf("download backup object %q: %w", objectKey, err)
	}
	return rc, objectKey, nil
}

func resolveRedisURL(envPath, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), nil
	}
	envMap, err := loadEnvMap(envPath)
	if err != nil {
		return "", err
	}
	redisURL := strings.TrimSpace(envMap["IMSUB_REDIS_URL"])
	if redisURL == "" {
		return "", fmt.Errorf("%w in %s: IMSUB_REDIS_URL", errRedisConfigMissing, envPath)
	}
	return redisURL, nil
}

func loadBackupConfig(envPath string) (backupConfig, error) {
	envMap, err := loadEnvMap(envPath)
	if err != nil {
		return backupConfig{}, err
	}
	cfg := backupConfig{
		S3Endpoint:        strings.TrimSpace(envMap["IMSUB_S3_ENDPOINT"]),
		S3Bucket:          strings.TrimSpace(envMap["IMSUB_S3_BUCKET"]),
		S3AccessKeyID:     strings.TrimSpace(envMap["IMSUB_S3_ACCESS_KEY_ID"]),
		S3SecretAccessKey: strings.TrimSpace(envMap["IMSUB_S3_SECRET_ACCESS_KEY"]),
		S3Region:          strings.TrimSpace(envMap["IMSUB_S3_REGION"]),
	}
	if cfg.S3Region == "" {
		cfg.S3Region = "auto"
	}

	missing := make([]string, 0, 4)
	for key, value := range map[string]string{
		"IMSUB_S3_ENDPOINT":          cfg.S3Endpoint,
		"IMSUB_S3_BUCKET":            cfg.S3Bucket,
		"IMSUB_S3_ACCESS_KEY_ID":     cfg.S3AccessKeyID,
		"IMSUB_S3_SECRET_ACCESS_KEY": cfg.S3SecretAccessKey,
	} {
		if value == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return backupConfig{}, fmt.Errorf("%w in %s: %s", errBackupConfigMissing, envPath, strings.Join(missing, ", "))
	}
	return cfg, nil
}

func loadMTProtoConfig(envPath string) (mtprotoConfig, error) {
	envMap, err := loadEnvMap(envPath)
	if err != nil {
		return mtprotoConfig{}, err
	}

	appIDRaw := strings.TrimSpace(envMap["IMSUB_TELEGRAM_MTPROTO_API_ID"])
	appHash := strings.TrimSpace(envMap["IMSUB_TELEGRAM_MTPROTO_API_HASH"])
	missing := make([]string, 0, 2)
	if appIDRaw == "" {
		missing = append(missing, "IMSUB_TELEGRAM_MTPROTO_API_ID")
	}
	if appHash == "" {
		missing = append(missing, "IMSUB_TELEGRAM_MTPROTO_API_HASH")
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return mtprotoConfig{}, fmt.Errorf("%w in %s: %s", errMTProtoConfigMissing, envPath, strings.Join(missing, ", "))
	}

	appID, err := strconv.Atoi(appIDRaw)
	if err != nil {
		return mtprotoConfig{}, fmt.Errorf("parse IMSUB_TELEGRAM_MTPROTO_API_ID in %s: %w", envPath, err)
	}
	if appID <= 0 {
		return mtprotoConfig{}, fmt.Errorf("%w in %s: must be > 0", errInvalidMTProtoAppID, envPath)
	}
	return mtprotoConfig{
		AppID:   appID,
		AppHash: appHash,
	}, nil
}

func loadEnvMap(envPath string) (map[string]string, error) {
	envMap, err := godotenv.Read(envPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", envPath, err)
	}
	return envMap, nil
}

func cleanOutputPath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

type backupObjectResolver interface {
	ListPrefix(context.Context, string) ([]s3.ObjectInfo, error)
}

type backupObjectStore interface {
	backupObjectResolver
	Download(context.Context, string) (io.ReadCloser, error)
}

func resolveBackupObjectKey(ctx context.Context, resolver backupObjectResolver, explicitKey string) (string, error) {
	objectKey := strings.TrimSpace(explicitKey)
	if objectKey != "" {
		return objectKey, nil
	}
	if resolver == nil {
		return "", errNoBackupObjects
	}
	objects, err := resolver.ListPrefix(ctx, "backups/")
	if err != nil {
		return "", fmt.Errorf("list backup objects: %w", err)
	}
	return selectLatestBackupKey(objects)
}

func selectLatestBackupKey(objects []s3.ObjectInfo) (string, error) {
	if len(objects) == 0 {
		return "", errNoBackupObjects
	}
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].LastModified.Equal(objects[j].LastModified) {
			return objects[i].Key > objects[j].Key
		}
		return objects[i].LastModified.After(objects[j].LastModified)
	})
	return objects[0].Key, nil
}

func usageError(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w: %s\nusage:\n  imsub-admin backup-download [-env .env] [-key backups/...jsonl.gz] -out tmp/backups/latest.jsonl.gz\n  imsub-admin backup-load [-env .env] [-key backups/...jsonl.gz] [-from-file tmp/backups/latest.jsonl.gz] [-redis-url redis://default:@redis:6379/0] -confirm=%s\n  imsub-admin mtproto-session [-env .env.dev] [-phone +123456789]", errUsage, msg, backupLoadConfirmValue)
}

type interactiveAuth struct {
	reader *bufio.Reader
	writer io.Writer
	phone  string
}

func (a *interactiveAuth) Phone(context.Context) (string, error) {
	if a.phone != "" {
		return a.phone, nil
	}
	return a.prompt("Telegram phone number: ")
}

func (a *interactiveAuth) Password(context.Context) (string, error) {
	return a.prompt("Telegram 2FA password (leave empty if not enabled): ")
}

func (a *interactiveAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errMTProtoSignupUnsupported
}

func (a *interactiveAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errMTProtoSignupUnsupported
}

func (a *interactiveAuth) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	if sentCode != nil {
		if _, err := fmt.Fprintf(a.writer, "Code sent via %s\n", sentCode.GetType().TypeName()); err != nil {
			return "", fmt.Errorf("write sent-code hint: %w", err)
		}
	}
	return a.prompt("Telegram login code: ")
}

func (a *interactiveAuth) prompt(label string) (string, error) {
	if _, err := fmt.Fprint(a.writer, label); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	line, err := a.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read prompt response: %w", err)
	}
	return strings.TrimSpace(line), nil
}
