package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"sort"
	"strings"

	"imsub/internal/adapter/redis"
	"imsub/internal/adapter/s3"

	"github.com/joho/godotenv"
)

const restoreConfirmValue = "restore-imsub"

var (
	errRestoreConfirmRequired = errors.New("restore requires explicit confirmation")
	errRestoreConfigMissing   = errors.New("missing restore env vars")
	errNoBackupObjects        = errors.New("no backup objects found under backups/")
	errUsage                  = errors.New("usage")
)

type restoreConfig struct {
	RedisURL          string
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Region          string
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
	case "restore":
		return runRestore(args[1:])
	default:
		return usageError("unknown command %q", args[0])
	}
}

func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	envPath := fs.String("env", ".env", "path to .env file")
	key := fs.String("key", "", "backup object key to restore")
	confirm := fs.String("confirm", "", "required confirmation value")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse restore flags: %w", err)
	}
	if strings.TrimSpace(*confirm) != restoreConfirmValue {
		return fmt.Errorf("%w: -confirm=%s", errRestoreConfirmRequired, restoreConfirmValue)
	}

	cfg, err := loadRestoreConfig(*envPath)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	store, err := redis.NewStore(cfg.RedisURL, logger)
	if err != nil {
		return fmt.Errorf("new redis store: %w", err)
	}
	defer func() { _ = store.Close() }()

	s3Client, err := s3.NewClient(cfg.S3Endpoint, cfg.S3Bucket, cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3Region)
	if err != nil {
		return fmt.Errorf("new s3 client: %w", err)
	}

	ctx := context.Background()
	objectKey := strings.TrimSpace(*key)
	if objectKey == "" {
		objects, err := s3Client.ListPrefix(ctx, "backups/")
		if err != nil {
			return fmt.Errorf("list backup objects: %w", err)
		}
		objectKey, err = selectLatestBackupKey(objects)
		if err != nil {
			return err
		}
	}

	rc, err := s3Client.Download(ctx, objectKey)
	if err != nil {
		return fmt.Errorf("download backup object %q: %w", objectKey, err)
	}
	defer func() { _ = rc.Close() }()

	restored, err := store.RestoreBackup(ctx, rc)
	if err != nil {
		return fmt.Errorf("restore backup %q: %w", objectKey, err)
	}

	if _, err := fmt.Fprintf(os.Stdout, "restored_keys=%d backup_key=%s\n", restored, objectKey); err != nil {
		return fmt.Errorf("write restore summary: %w", err)
	}
	return nil
}

func loadRestoreConfig(envPath string) (restoreConfig, error) {
	envMap, err := godotenv.Read(envPath)
	if err != nil {
		return restoreConfig{}, fmt.Errorf("read %s: %w", envPath, err)
	}
	cfg := restoreConfig{
		RedisURL:          strings.TrimSpace(envMap["IMSUB_REDIS_URL"]),
		S3Endpoint:        strings.TrimSpace(envMap["IMSUB_S3_ENDPOINT"]),
		S3Bucket:          strings.TrimSpace(envMap["IMSUB_S3_BUCKET"]),
		S3AccessKeyID:     strings.TrimSpace(envMap["IMSUB_S3_ACCESS_KEY_ID"]),
		S3SecretAccessKey: strings.TrimSpace(envMap["IMSUB_S3_SECRET_ACCESS_KEY"]),
		S3Region:          strings.TrimSpace(envMap["IMSUB_S3_REGION"]),
	}
	if cfg.S3Region == "" {
		cfg.S3Region = "auto"
	}

	missing := make([]string, 0, 5)
	for key, value := range map[string]string{
		"IMSUB_REDIS_URL":            cfg.RedisURL,
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
		return restoreConfig{}, fmt.Errorf("%w in %s: %s", errRestoreConfigMissing, envPath, strings.Join(missing, ", "))
	}
	return cfg, nil
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
	return fmt.Errorf("%w: %s\nusage: imsub-admin restore [-env .env] [-key backups/...jsonl.gz] -confirm=%s", errUsage, msg, restoreConfirmValue)
}
