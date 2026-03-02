package redis

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
		return core.UserIdentity{}, false, err
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
		return displacedUserID, err
	}
	return displacedUserID, nil
}

// SaveUserCreator links a Twitch account and binds a creator membership atomically.
func (s *Store) SaveUserCreator(ctx context.Context, telegramUserID int64, creatorID, twitchUserID, twitchLogin, language string) (displacedUserID int64, err error) {
	displacedUserID, err = s.prepareTwitchLink(ctx, telegramUserID, twitchUserID)
	if err != nil {
		return 0, err
	}

	_, err = linkViewerCreatorScript.Run(ctx, s.rdb,
		[]string{
			keyUserIdentity(telegramUserID),
			keyTwitchToTelegram(twitchUserID),
			keyCreatorMembers(creatorID),
			keyUsersSet(),
			keyUserCreators(telegramUserID),
		},
		strconv.FormatInt(telegramUserID, 10),
		twitchUserID,
		twitchLogin,
		language,
		time.Now().UTC().Format(time.RFC3339),
		creatorID,
	).Result()

	if err != nil {
		if isDifferentTwitchLinkError(err) {
			return displacedUserID, core.ErrDifferentTwitch
		}
		return displacedUserID, err
	}
	return displacedUserID, nil
}

// UserCreatorIDs returns the creator IDs associated with a Telegram user, with reverse-index backfill.
func (s *Store) UserCreatorIDs(ctx context.Context, telegramUserID int64) ([]string, error) {
	ids, err := s.rdb.SMembers(ctx, keyUserCreators(telegramUserID)).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) != 0 {
		slices.Sort(ids)
		return ids, nil
	}

	allIDs, err := s.rdb.SMembers(ctx, keyCreatorsSet()).Result()
	if err != nil {
		return nil, err
	}
	if len(allIDs) == 0 {
		return nil, nil
	}
	tgStr := strconv.FormatInt(telegramUserID, 10)
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.BoolCmd, len(allIDs))
	for i, creatorID := range allIDs {
		cmds[i] = pipe.SIsMember(ctx, keyCreatorMembers(creatorID), tgStr)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	fallbackIDs := make([]string, 0, len(allIDs))
	for i, creatorID := range allIDs {
		if cmds[i].Val() {
			fallbackIDs = append(fallbackIDs, creatorID)
		}
	}
	if len(fallbackIDs) == 0 {
		return nil, nil
	}
	slices.Sort(fallbackIDs)
	args := make([]any, len(fallbackIDs))
	for i, creatorID := range fallbackIDs {
		args[i] = creatorID
	}
	if err := s.rdb.SAdd(ctx, keyUserCreators(telegramUserID), args...).Err(); err != nil {
		s.log().Warn("UserCreatorIDs reverse-index backfill failed", "telegram_user_id", telegramUserID, "error", err)
	}
	return fallbackIDs, nil
}

// RemoveUserCreatorByTelegram removes a user's membership from a creator group.
func (s *Store) RemoveUserCreatorByTelegram(ctx context.Context, telegramUserID int64, creatorID string) error {
	tgStr := strconv.FormatInt(telegramUserID, 10)
	pipe := s.rdb.TxPipeline()
	pipe.SRem(ctx, keyCreatorMembers(creatorID), tgStr)
	pipe.SRem(ctx, keyUserCreators(telegramUserID), creatorID)
	_, err := pipe.Exec(ctx)
	return err
}

// AddUserCreatorMembership adds a user to a creator's member set and reverse index.
func (s *Store) AddUserCreatorMembership(ctx context.Context, telegramUserID int64, creatorID string) error {
	tgStr := strconv.FormatInt(telegramUserID, 10)
	pipe := s.rdb.TxPipeline()
	pipe.SAdd(ctx, keyCreatorMembers(creatorID), tgStr)
	pipe.SAdd(ctx, keyUserCreators(telegramUserID), creatorID)
	_, err := pipe.Exec(ctx)
	return err
}

// RemoveUserCreatorByTwitch resolves a Twitch user to Telegram and removes their creator membership.
func (s *Store) RemoveUserCreatorByTwitch(ctx context.Context, twitchUserID, creatorID string) (telegramUserID int64, found bool, err error) {
	tgStr, err := s.rdb.Get(ctx, keyTwitchToTelegram(twitchUserID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, err
	}
	tgID, err := strconv.ParseInt(tgStr, 10, 64)
	if err != nil {
		return 0, false, err
	}
	if err := s.RemoveUserCreatorByTelegram(ctx, tgID, creatorID); err != nil {
		return 0, false, err
	}
	return tgID, true, nil
}

// DeleteAllUserData removes all stored data for a Telegram user.
func (s *Store) DeleteAllUserData(ctx context.Context, telegramUserID int64) error {
	creatorIDs, err := s.UserCreatorIDs(ctx, telegramUserID)
	if err != nil {
		return err
	}
	identity, ok, err := s.UserIdentity(ctx, telegramUserID)
	if err != nil {
		return err
	}

	pipe := s.rdb.TxPipeline()
	tgStr := strconv.FormatInt(telegramUserID, 10)
	for _, creatorID := range creatorIDs {
		pipe.SRem(ctx, keyCreatorMembers(creatorID), tgStr)
	}
	pipe.Del(ctx, keyUserCreators(telegramUserID))
	pipe.Del(ctx, keyUserIdentity(telegramUserID))
	pipe.SRem(ctx, keyUsersSet(), tgStr)
	if ok && identity.TwitchUserID != "" {
		pipe.Del(ctx, keyTwitchToTelegram(identity.TwitchUserID))
	}
	_, err = pipe.Exec(ctx)
	return err
}
