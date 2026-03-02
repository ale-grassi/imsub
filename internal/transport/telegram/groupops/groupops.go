package groupops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"imsub/internal/core"
	"imsub/internal/transport/telegram/tgerr"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

type limiter interface {
	Wait(ctx context.Context, chatID int64) error
}

type creatorStore interface {
	ListActiveCreators(ctx context.Context) ([]core.Creator, error)
}

// Client wraps Telegram group-level operations used by business flows.
type Client struct {
	bot     *telego.Bot
	limiter limiter
	logger  *slog.Logger
	store   creatorStore
}

// New creates a Telegram group operations client.
func New(bot *telego.Bot, lim limiter, logger *slog.Logger, store creatorStore) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		bot:     bot,
		limiter: lim,
		logger:  logger,
		store:   store,
	}
}

// CreateInviteLink creates a single-use, join-request invite link for
// groupChatID that expires in 10 minutes.
func (c *Client) CreateInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	if c == nil || c.bot == nil {
		return "", errors.New("telegram bot not initialized")
	}
	expire := time.Now().Add(10 * time.Minute).Unix()
	linkName := fmt.Sprintf("imsub-%d-%s", telegramUserID, name)
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			return "", err
		}
	}
	result, err := c.bot.CreateChatInviteLink(ctx, &telego.CreateChatInviteLinkParams{
		ChatID:             tu.ID(groupChatID),
		CreatesJoinRequest: true,
		ExpireDate:         expire,
		Name:               linkName,
	})
	if err != nil {
		return "", err
	}
	if result == nil || result.InviteLink == "" {
		return "", errors.New("telegram returned empty invite link")
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
func (c *Client) KickFromGroup(ctx context.Context, groupChatID int64, telegramUserID int64) error {
	if c == nil || c.bot == nil {
		return nil
	}
	until := time.Now().Add(60 * time.Second).Unix()
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			return err
		}
	}
	err := c.bot.BanChatMember(ctx, &telego.BanChatMemberParams{
		ChatID:    tu.ID(groupChatID),
		UserID:    telegramUserID,
		UntilDate: until,
	})
	if err != nil {
		if tgerr.IsForbidden(err) || tgerr.IsBadRequest(err) {
			return nil
		}
		return err
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, groupChatID); err != nil {
			return err
		}
	}
	err = c.bot.UnbanChatMember(ctx, &telego.UnbanChatMemberParams{
		ChatID:       tu.ID(groupChatID),
		UserID:       telegramUserID,
		OnlyIfBanned: true,
	})
	return err
}

// KickDisplacedUser removes telegramUserID from every active creator group.
func (c *Client) KickDisplacedUser(ctx context.Context, telegramUserID int64) {
	if c == nil || c.store == nil {
		return
	}
	creators, err := c.store.ListActiveCreators(ctx)
	if err != nil {
		c.logger.Warn("kick displaced user listActiveCreators failed", "error", err)
		return
	}
	for _, creator := range creators {
		if creator.GroupChatID == 0 {
			continue
		}
		if err := c.KickFromGroup(ctx, creator.GroupChatID, telegramUserID); err != nil {
			c.logger.Warn("kick displaced user from group failed", "group_chat_id", creator.GroupChatID, "telegram_user_id", telegramUserID, "error", err)
		}
	}
}
