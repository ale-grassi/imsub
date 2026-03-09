package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"imsub/internal/core"

	"github.com/redis/go-redis/v9"
)

const luaErrDifferentTwitch = "DIFFERENT_TWITCH"

func isDifferentTwitchLinkError(err error) bool {
	if err == nil {
		return false
	}
	var redisErr redis.Error
	if !errors.As(err, &redisErr) {
		return false
	}
	msg := strings.TrimSpace(redisErr.Error())
	return msg == luaErrDifferentTwitch || msg == "ERR "+luaErrDifferentTwitch
}

// --- User identity ---

// UserIdentity returns the linked Twitch identity for a Telegram user, or false if unlinked.
func (s *Store) UserIdentity(ctx context.Context, telegramUserID int64) (core.UserIdentity, bool, error) {
	vals, err := s.rdb.HGetAll(ctx, keyUserIdentity(telegramUserID)).Result()
	if err != nil {
		return core.UserIdentity{}, false, fmt.Errorf("redis hgetall user identity: %w", err)
	}
	if len(vals) == 0 {
		return core.UserIdentity{}, false, nil
	}
	verifiedAt, err := time.Parse(time.RFC3339, vals["verified_at"])
	if err != nil {
		s.log().Warn("UserIdentity invalid verified_at, using current time",
			"telegram_user_id", telegramUserID,
			"verified_at_raw", vals["verified_at"],
			"error", err,
		)
		verifiedAt = time.Now().UTC()
	}
	return core.UserIdentity{
		TelegramUserID: telegramUserID,
		TwitchUserID:   vals["twitch_user_id"],
		TwitchLogin:    vals["twitch_login"],
		Language:       vals["language"],
		VerifiedAt:     verifiedAt,
	}, true, nil
}

func (s *Store) prepareTwitchLink(ctx context.Context, telegramUserID int64, twitchUserID string) (displacedUserID int64, err error) {
	existing, ok, err := s.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return 0, err
	}
	if ok && existing.TwitchUserID != "" && existing.TwitchUserID != twitchUserID {
		return 0, core.ErrDifferentTwitch
	}

	existingTg, err := s.rdb.Get(ctx, keyTwitchToTelegram(twitchUserID)).Result()
	switch {
	case errors.Is(err, redis.Nil):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("lookup existing twitch mapping: %w", err)
	}
	if existingTg == strconv.FormatInt(telegramUserID, 10) {
		return 0, nil
	}

	oldTgID, err := strconv.ParseInt(existingTg, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse existing telegram user id %q: %w", existingTg, err)
	}
	if oldTgID == 0 {
		return 0, nil
	}
	if err := s.DeleteAllUserData(ctx, oldTgID); err != nil {
		return 0, fmt.Errorf("delete displaced user data: %w", err)
	}
	return oldTgID, nil
}

// SaveUserIdentityOnly links a Twitch account to a Telegram user without creator binding.
func (s *Store) SaveUserIdentityOnly(ctx context.Context, telegramUserID int64, twitchUserID, twitchLogin, language string) (displacedUserID int64, err error) {
	displacedUserID, err = s.prepareTwitchLink(ctx, telegramUserID, twitchUserID)
	if err != nil {
		return 0, err
	}

	_, err = linkViewerIdentityScript.Run(ctx, s.rdb,
		[]string{
			keyUserIdentity(telegramUserID),
			keyTwitchToTelegram(twitchUserID),
			keyUsersSet(),
		},
		strconv.FormatInt(telegramUserID, 10),
		twitchUserID,
		twitchLogin,
		language,
		time.Now().UTC().Format(time.RFC3339),
	).Result()

	if err != nil {
		if isDifferentTwitchLinkError(err) {
			return displacedUserID, core.ErrDifferentTwitch
		}
		return displacedUserID, fmt.Errorf("eval link viewer identity script: %w", err)
	}
	return displacedUserID, nil
}

// RemoveUserCreatorByTwitch resolves a Twitch user to Telegram and removes their creator membership.
func (s *Store) RemoveUserCreatorByTwitch(ctx context.Context, twitchUserID, creatorID string) (telegramUserID int64, found bool, err error) {
	tgStr, err := s.rdb.Get(ctx, keyTwitchToTelegram(twitchUserID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("redis get twitch mapping: %w", err)
	}
	tgID, err := strconv.ParseInt(tgStr, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse telegram user id: %w", err)
	}
	groups, err := s.ListManagedGroupsByCreator(ctx, creatorID)
	if err != nil {
		return 0, false, err
	}
	for _, group := range groups {
		if err := s.RemoveTrackedGroupMember(ctx, group.ChatID, tgID); err != nil {
			return 0, false, err
		}
	}
	return tgID, true, nil
}

// DeleteAllUserData removes all stored data for a Telegram user.
func (s *Store) DeleteAllUserData(ctx context.Context, telegramUserID int64) error {
	identity, ok, err := s.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return err
	}

	pipe := s.rdb.TxPipeline()
	tgStr := strconv.FormatInt(telegramUserID, 10)
	trackedGroups, err := s.rdb.SMembers(ctx, keyUserTrackedGroups(telegramUserID)).Result()
	if err != nil {
		return fmt.Errorf("redis smembers user tracked groups: %w", err)
	}
	for _, rawChatID := range trackedGroups {
		chatID, parseErr := strconv.ParseInt(rawChatID, 10, 64)
		if parseErr != nil {
			s.log().Warn("DeleteAllUserData invalid tracked group id, skipping cleanup", "telegram_user_id", telegramUserID, "group_chat_id_raw", rawChatID, "error", parseErr)
			continue
		}
		pipe.SRem(ctx, keyTrackedGroupMembers(chatID), tgStr)
		pipe.Del(ctx, keyTrackedGroupMemberMeta(chatID, telegramUserID))
	}
	pipe.Del(ctx, keyUserTrackedGroups(telegramUserID))
	pipe.Del(ctx, keyUserIdentity(telegramUserID))
	pipe.SRem(ctx, keyUsersSet(), tgStr)
	if ok && identity.TwitchUserID != "" {
		pipe.Del(ctx, keyTwitchToTelegram(identity.TwitchUserID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis exec delete all user data: %w", err)
	}
	return nil
}
