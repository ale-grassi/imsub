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
	if result.Kind == core.MemberCleanupKindGroupUnregistration {
		return nil
	}
	lang := "en"
	if identity, ok, err := c.store.UserIdentity(ctx, result.OwnerTelegramID); err == nil && ok && identity.Language != "" {
		lang = i18n.NormalizeLanguage(identity.Language)
	}
	view, ok := buildMemberCleanupResultView(lang, result)
	if !ok {
		return nil
	}
	if messageID := c.sendMsg(ctx, result.OwnerTelegramID, view.text, &view.opts); messageID == 0 {
		return errMemberCleanupNotificationSend
	}
	return nil
}

func buildMemberCleanupResultView(lang string, result core.MemberCleanupResult) (sharedView, bool) {
	if result.FailedCount == 0 || result.Kind != core.MemberCleanupKindCreatorReset {
		return sharedView{}, false
	}

	args := []any{
		html.EscapeString(result.CreatorLogin),
		renderResetViewerGroups(lang, result.GroupNames),
		result.FailedCount,
	}
	return sharedView{text: fmt.Sprintf(i18n.Translate(lang, msgCleanupResetWarningDM), args...)}, true
}
