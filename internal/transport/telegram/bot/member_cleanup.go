package bot

import (
	"context"
	"errors"
	"fmt"
	"html"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
)

var errMemberCleanupNotificationSend = errors.New("send member cleanup completion dm")

// NotifyMemberCleanupComplete sends a creator DM when queued membership cleanup finishes.
func (c *Bot) NotifyMemberCleanupComplete(ctx context.Context, result core.MemberCleanupResult) error {
	lang := "en"
	if identity, ok, err := c.store.UserIdentity(ctx, result.OwnerTelegramID); err == nil && ok && identity.Language != "" {
		lang = i18n.NormalizeLanguage(identity.Language)
	}
	view := buildMemberCleanupResultView(lang, result)
	if messageID := c.sendMsg(ctx, result.OwnerTelegramID, view.text, &view.opts); messageID == 0 {
		return errMemberCleanupNotificationSend
	}
	return nil
}

func buildMemberCleanupResultView(lang string, result core.MemberCleanupResult) sharedView {
	key := msgCleanupGroupDoneDM
	args := []any{
		html.EscapeString(result.GroupName),
		result.TargetedCount,
		result.SucceededCount,
		result.FailedCount,
	}
	if result.Kind == core.MemberCleanupKindCreatorReset {
		key = msgCleanupResetDoneDM
		args = []any{
			html.EscapeString(result.CreatorLogin),
			result.ManagedGroupCount,
			result.TargetedCount,
			result.SucceededCount,
			result.FailedCount,
		}
	}
	if result.FailedCount > 0 && result.SucceededCount > 0 {
		if result.Kind == core.MemberCleanupKindCreatorReset {
			key = msgCleanupResetPartialDM
		} else {
			key = msgCleanupGroupPartialDM
		}
	}
	if result.FailedCount > 0 && result.SucceededCount == 0 {
		if result.Kind == core.MemberCleanupKindCreatorReset {
			key = msgCleanupResetFailedDM
		} else {
			key = msgCleanupGroupFailedDM
		}
	}
	return sharedView{text: fmt.Sprintf(i18n.Translate(lang, key), args...)}
}
