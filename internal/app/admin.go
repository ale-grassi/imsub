package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"imsub/internal/adapter/redis"
	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/config"
	"imsub/internal/platform/i18n"
	"imsub/internal/platform/observability"
	"imsub/internal/platform/ratelimit"
	telegramgroups "imsub/internal/transport/telegram/groups"
	telegrammtproto "imsub/internal/transport/telegram/mtproto"

	"github.com/mymmrac/telego"
)

const adminMemberTagsRefreshUsage = "imsub admin member-tags-refresh -chat-id <group_chat_id> [-timeout 2m]"

var (
	errAdminUsage                 = errors.New("admin usage")
	errAdminChatIDNeeded          = errors.New("chat-id is required")
	errAdminMTProtoConfigRequired = errors.New("mtproto config is required for full member-tags refresh")
)

type memberTagsRefreshOptions struct {
	ChatID  int64
	Timeout time.Duration
}

// RunAdminCommand runs private operator-only commands for the app binary.
// These commands are intentionally not exposed through Telegram, HTTP, or UI surfaces.
func RunAdminCommand(args []string, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}
	if len(args) == 0 {
		return adminUsageError("missing admin command")
	}

	switch args[0] {
	case "member-tags-refresh":
		opts, err := parseMemberTagsRefreshOptions(args[1:])
		if err != nil {
			return err
		}
		return runMemberTagsRefresh(opts, stdout)
	default:
		return adminUsageError("unknown admin command %q", args[0])
	}
}

func parseMemberTagsRefreshOptions(args []string) (memberTagsRefreshOptions, error) {
	fs := flag.NewFlagSet("member-tags-refresh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := memberTagsRefreshOptions{Timeout: 2 * time.Minute}
	fs.Int64Var(&opts.ChatID, "chat-id", 0, "managed group chat ID")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "operation timeout")
	if err := fs.Parse(args); err != nil {
		return memberTagsRefreshOptions{}, fmt.Errorf("parse member-tags-refresh flags: %w", err)
	}
	if fs.NArg() > 0 {
		return memberTagsRefreshOptions{}, adminUsageError("unexpected argument %q", fs.Arg(0))
	}
	if opts.ChatID == 0 {
		return memberTagsRefreshOptions{}, fmt.Errorf("%w: %s", errAdminChatIDNeeded, adminMemberTagsRefreshUsage)
	}
	if opts.Timeout <= 0 {
		return memberTagsRefreshOptions{}, adminUsageError("timeout must be > 0")
	}
	return opts, nil
}

func runMemberTagsRefresh(opts memberTagsRefreshOptions, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	ctx = events.WithForegroundOperationContext(ctx, "admin_member_tags_refresh")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}
	if err := i18n.Ensure(); err != nil {
		return fmt.Errorf("i18n init failed: %w", err)
	}
	if !cfg.MTProtoEnabled() {
		return errAdminMTProtoConfigRequired
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: adminLogLevel(cfg.DebugLogs)}))
	store, err := redis.NewStore(cfg.RedisURL, logger)
	if err != nil {
		return fmt.Errorf("create redis store: %w", err)
	}
	defer func() { _ = store.Close() }()

	bot, err := telego.NewBot(cfg.TelegramBotToken, telego.WithAPICaller(newTelegramAPICaller()))
	if err != nil {
		return fmt.Errorf("create telegram bot: %w", err)
	}
	limiter := ratelimit.NewRateLimiter(25, time.Second)
	defer limiter.Close()

	mtprotoClient, err := telegrammtproto.New(cfg.TelegramMTProtoAppID, cfg.TelegramMTProtoHash, cfg.TelegramMTProtoSession)
	if err != nil {
		return fmt.Errorf("create mtproto client: %w", err)
	}
	selfID, err := mtprotoClient.Validate(ctx)
	if err != nil {
		return fmt.Errorf("validate mtproto client: %w", err)
	}

	eventSink := observability.NewEventLogger(logger)
	godAccess := core.NewGodAccessChecker(cfg.GodTelegramUserIDs...).WithIDs(selfID)
	tgGroups := telegramgroups.New(bot, limiter, logger, store, eventSink)
	bootstrap := core.NewGroupBootstrapService(store, tgGroups, mtprotoClient, godAccess, eventSink, logger)
	syncer := core.NewMemberTagSyncService(store, tgGroups, bootstrap, mtprotoClient, logger)

	counts, err := syncer.SyncGroup(ctx, opts.ChatID, true)
	if err != nil {
		return fmt.Errorf("refresh member tags for chat %d: %w", opts.ChatID, err)
	}
	summary := fmt.Sprintf("chat_id=%d full_refresh=true groups=%d set=%d cleared=%d noop=%d errors=%d tracked_stored=%d untracked_stored=%d desired_tracked=%d desired_untracked=%d existing_tags=%d snapshot_members=%d snapshot_filtered_tracked=%d snapshot_filtered_untracked=%d\n", opts.ChatID, counts.Groups, counts.Set, counts.Cleared, counts.Noop, counts.Errors, counts.TrackedStored, counts.UntrackedStored, counts.DesiredTracked, counts.DesiredUntracked, counts.ExistingTags, counts.SnapshotMembers, counts.SnapshotFilteredTracked, counts.SnapshotFilteredUntracked)
	// #nosec G705 -- private CLI writes fixed-format numeric diagnostics to the operator-provided stdout writer.
	if _, err := io.WriteString(stdout, summary); err != nil {
		return fmt.Errorf("write member-tags refresh summary: %w", err)
	}
	return nil
}

func adminLogLevel(debug bool) slog.Level {
	if debug {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func adminUsageError(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w: %s\nusage:\n  %s", errAdminUsage, strings.TrimSpace(msg), adminMemberTagsRefreshUsage)
}
