package flows

import (
	"context"

	"imsub/internal/platform/i18n"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
)

// onResetCommand handles /reset by showing the reset confirmation prompt.
func (c *Controller) onResetCommand(ctx *tghandler.Context, message telego.Message) error {
	lang := i18n.NormalizeLanguage(message.From.LanguageCode)
	c.renderResetPrompt(ctx, message.From.ID, 0, lang, resetOriginCommand)
	return nil
}

func (c *Controller) handleResetAction(ctx context.Context, telegramUserID int64, editMsgID int, lang string, action callbackAction) string {
	switch action.verb {
	case callbackVerbOpen:
		return c.renderResetPrompt(ctx, telegramUserID, editMsgID, lang, action.origin)
	case callbackVerbPick:
		return c.renderResetConfirm(ctx, telegramUserID, editMsgID, lang, action.origin, action.scope)
	case callbackVerbBack:
		return c.handleResetBack(ctx, telegramUserID, editMsgID, lang, action.origin)
	case callbackVerbMenu:
		return c.handleResetBackToMenu(ctx, telegramUserID, editMsgID, lang, action.origin)
	case callbackVerbCancel:
		return c.handleResetCancel(ctx, telegramUserID, editMsgID, lang)
	case callbackVerbExecute:
		return c.executeReset(ctx, telegramUserID, editMsgID, lang, action.scope)
	case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect:
		// parseCallbackAction rejects these verbs for reset callbacks.
		c.log().Warn("unsupported reset callback verb", "telegram_user_id", telegramUserID, "verb", action.verb)
		return ""
	default:
		c.log().Warn("unsupported reset callback verb", "telegram_user_id", telegramUserID, "verb", action.verb)
		return ""
	}
}

// renderResetPrompt is the entry point for /reset and reset callbacks.
func (c *Controller) renderResetPrompt(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin) string {
	scopes, err := c.app.Reset.LoadScopes(ctx, telegramUserID)
	if err != nil {
		view := buildResetErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	if view, ok := buildResetPromptView(lang, scopes, origin); ok {
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return ""
	}

	if scopes.HasIdentity {
		return c.renderResetConfirm(ctx, telegramUserID, editMsgID, lang, origin, resetScopeViewer)
	}
	return c.renderResetConfirm(ctx, telegramUserID, editMsgID, lang, origin, resetScopeCreator)
}

func (c *Controller) renderResetConfirm(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin, scope resetScope) string {
	scopes, err := c.app.Reset.LoadScopes(ctx, telegramUserID)
	if err != nil {
		view := buildResetErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	view := c.buildResetConfirmView(ctx, telegramUserID, lang, scopes, scope)
	if view.text == "" {
		emptyView := buildResetEmptyView(lang)
		c.reply(ctx, telegramUserID, editMsgID, emptyView.text, &emptyView.opts)
		return ""
	}
	replyView := buildResetConfirmReply(lang, view, origin, scope)
	c.reply(ctx, telegramUserID, editMsgID, replyView.text, &replyView.opts)
	return ""
}

func (c *Controller) handleResetBack(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin) string {
	scopes, err := c.app.Reset.LoadScopes(ctx, telegramUserID)
	if err != nil {
		view := buildMainMenuTextView(lang, msgErrReset)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	if scopes.HasIdentity && scopes.HasCreator {
		return c.renderResetPrompt(ctx, telegramUserID, editMsgID, lang, origin)
	}

	switch origin {
	case resetOriginViewer:
		return c.handleViewerStart(ctx, telegramUserID, editMsgID, lang)
	case resetOriginCreator:
		return c.handleCreatorStart(ctx, telegramUserID, editMsgID, lang)
	case resetOriginCommand:
		return c.handleResetCancel(ctx, telegramUserID, editMsgID, lang)
	}
	return ""
}

func (c *Controller) handleResetBackToMenu(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin) string {
	switch origin {
	case resetOriginViewer:
		return c.handleViewerStart(ctx, telegramUserID, editMsgID, lang)
	case resetOriginCreator:
		return c.handleCreatorStart(ctx, telegramUserID, editMsgID, lang)
	case resetOriginCommand:
		return c.handleResetCancel(ctx, telegramUserID, editMsgID, lang)
	}
	return ""
}

// handleResetCancel cleanly aborts the reset flow, removing buttons and showing a safe message.
func (c *Controller) handleResetCancel(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	view := buildHTMLTextView(lang, msgResetExitHTML)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Controller) executeReset(ctx context.Context, telegramUserID int64, editMsgID int, lang string, scope resetScope) string {
	switch scope {
	case resetScopeViewer:
		return c.handleResetViewerCommand(ctx, telegramUserID, editMsgID, lang)
	case resetScopeCreator:
		return c.handleResetCreatorCommand(ctx, telegramUserID, editMsgID, lang)
	case resetScopeBoth:
		return c.handleResetBothCommand(ctx, telegramUserID, editMsgID, lang)
	default:
		c.log().Warn("unsupported reset execute scope", "telegram_user_id", telegramUserID, "scope", scope)
		return ""
	}
}
