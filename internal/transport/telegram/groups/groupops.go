package groups

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/transport/telegram"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

var (
	errBotNotInitialized = errors.New("telegram bot not initialized")
	errEmptyInviteLink   = errors.New("telegram returned empty invite link")
)

type limiter interface {
	Wait(ctx context.Context, chatID int64) error
}

type creatorStore interface {
	ListManagedGroups(ctx context.Context) ([]core.ManagedGroup, error)
}

// Client wraps Telegram group-level operations used by business flows.
type Client struct {
	bot     *telego.Bot
	limiter limiter
	logger  *slog.Logger
	store   creatorStore
	events  events.EventSink
}

// New creates a Telegram group operations client.
func New(bot *telego.Bot, lim limiter, logger *slog.Logger, store creatorStore, sink events.EventSink) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		bot:     bot,
		limiter: lim,
		logger:  logger,
		store:   store,
		events:  events.EnsureSink(sink),
	}
}

// CreateInviteLink creates a single-use, join-request invite link for
// groupChatID that expires in 10 minutes.
func (c *Client) CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	if c == nil || c.bot == nil {
		return "", errBotNotInitialized
	}
	expire := time.Now().Add(10 * time.Minute).Unix()
	linkName := fmt.Sprintf("imsub-%d-%s", telegramUserID, name)
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			return "", fmt.Errorf("limiter wait: %w", err)
		}
	}
	result, err := c.bot.CreateChatInviteLink(ctx, &telego.CreateChatInviteLinkParams{
		ChatID:             tu.ID(groupChatID),
		CreatesJoinRequest: true,
		ExpireDate:         expire,
		Name:               linkName,
	})
	if err != nil {
		return "", fmt.Errorf("create chat invite link: %w", err)
	}
	if result == nil || result.InviteLink == "" {
		return "", errEmptyInviteLink
	}
	return result.InviteLink, nil
}

// IsGroupMember reports whether telegramUserID is a member/admin/creator/restricted in groupChatID.
func (c *Client) IsGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool {
	if c == nil || c.bot == nil {
		return false
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			return false
		}
	}
	member, err := c.bot.GetChatMember(ctx, &telego.GetChatMemberParams{
		ChatID: tu.ID(groupChatID),
		UserID: telegramUserID,
	})
	if err != nil {
		return false
	}
	switch member.MemberStatus() {
	case telego.MemberStatusMember, telego.MemberStatusAdministrator, telego.MemberStatusCreator, telego.MemberStatusRestricted:
		return true
	}
	return false
}

// KickFromGroup bans and immediately unbans telegramUserID from groupChatID.
func (c *Client) KickFromGroup(ctx context.Context, groupChatID int64, telegramUserID int64, reason core.KickReason) error {
	if c == nil || c.bot == nil {
		return nil
	}
	until := time.Now().Add(60 * time.Second).Unix()
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			c.recordKick(ctx, reason, "failed")
			return fmt.Errorf("limiter wait for ban: %w", err)
		}
	}
	err := c.bot.BanChatMember(ctx, &telego.BanChatMemberParams{
		ChatID:    tu.ID(groupChatID),
		UserID:    telegramUserID,
		UntilDate: until,
	})
	if err != nil {
		if telegram.IsForbidden(err) || telegram.IsBadRequest(err) {
			c.recordKick(ctx, reason, "skipped")
			return nil
		}
		c.recordKick(ctx, reason, "failed")
		return fmt.Errorf("ban chat member: %w", err)
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			c.recordKick(ctx, reason, "failed")
			return fmt.Errorf("limiter wait for unban: %w", err)
		}
	}
	err = c.bot.UnbanChatMember(ctx, &telego.UnbanChatMemberParams{
		ChatID:       tu.ID(groupChatID),
		UserID:       telegramUserID,
		OnlyIfBanned: true,
	})
	if err != nil {
		c.recordKick(ctx, reason, "failed")
		return fmt.Errorf("unban chat member: %w", err)
	}
	c.recordKick(ctx, reason, "ok")
	return nil
}

// KickDisplacedUser removes telegramUserID from every managed group.
func (c *Client) KickDisplacedUser(ctx context.Context, telegramUserID int64) {
	if c == nil || c.store == nil {
		return
	}
	groups, err := c.store.ListManagedGroups(ctx)
	if err != nil {
		c.logger.Warn("kick displaced user ListManagedGroups failed", "error", err)
		return
	}
	for _, group := range groups {
		if err := c.KickFromGroup(ctx, group.ChatID, telegramUserID, core.KickReasonDisplacedUser); err != nil {
			c.logger.Warn("kick displaced user from group failed", "group_chat_id", group.ChatID, "telegram_user_id", telegramUserID, "error", err)
		}
	}
}

func (c *Client) recordKick(ctx context.Context, reason core.KickReason, result string) {
	if c == nil || c.events == nil {
		return
	}
	c.events.Emit(ctx, events.Event{
		Name:    events.NameTelegramKickAction,
		Outcome: result,
		Fields: map[string]string{
			"reason": string(reason),
		},
	})
}
