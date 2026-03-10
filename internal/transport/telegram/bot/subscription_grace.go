package bot

import (
	"context"
	"errors"

	"imsub/internal/core"
)

var errSubscriptionGraceExpiredSend = errors.New("send subscription grace expired dm")

// NotifySubscriptionGraceExpired informs the viewer that their grace period has
// elapsed and access was removed.
func (c *Bot) NotifySubscriptionGraceExpired(ctx context.Context, result core.ExpiredSubscriptionGraceResult) error {
	view := buildSubscriptionGraceExpiredView(result.Language, result.ViewerLogin, result.BroadcasterLogin)
	if messageID := c.sendMsg(ctx, result.TelegramUserID, view.text, &view.opts); messageID == 0 {
		return errSubscriptionGraceExpiredSend
	}
	return nil
}
