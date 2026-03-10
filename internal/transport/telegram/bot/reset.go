package bot

import (
	"context"
	"fmt"
	"html"
	"strings"

	"imsub/internal/core"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	msgErrReset                        = "err_reset"
	msgResetNothingHTML                = "reset_nothing_html"
	msgResetDoneViewerHTML             = "reset_done_viewer_html"
	msgResetDoneCreatorHTML            = "reset_done_creator_html"
	msgResetDoneBothHTML               = "reset_done_both_html"
	msgResetChooseScopeHTML            = "reset_choose_scope_html"
	msgResetChooseCreatorActionCreator = "reset_choose_creator_action_creator_html"
	msgResetChooseCreatorActionBoth    = "reset_choose_creator_action_both_html"
	msgResetConfirmViewerHTML          = "reset_confirm_viewer_html"
	msgResetConfirmCreatorHTML         = "reset_confirm_creator_html"
	msgResetConfirmBothHTML            = "reset_confirm_both_html"
	msgResetExitHTML                   = "reset_exit_html"
	msgResetActionKeepLine             = "reset_action_keep_line"
	msgResetActionKickLine             = "reset_action_kick_line"
	msgResetCreatorGroupsLine          = "reset_creator_groups_line"
	msgResetCreatorKickTargetsLine     = "reset_creator_kick_targets_line"
	msgResetCreatorKickFailuresLine    = "reset_creator_kick_failures_line"
	btnResetKeepMembers                = "btn_reset_keep_members"
	btnResetKickTrackedMembers         = "btn_reset_kick_tracked_members"
)

type resetConfirmView struct{ text string }

// onResetCommand handles /reset by showing the reset confirmation prompt.
func (c *Bot) onResetCommand(ctx *tghandler.Context, message telego.Message) error {
	lang := i18n.NormalizeLanguage(message.From.LanguageCode)
	c.renderResetPrompt(ctx, message.From.ID, 0, lang, resetOriginCommand)
	return nil
}

func (c *Bot) handleResetAction(ctx context.Context, telegramUserID int64, editMsgID int, lang string, action callbackAction) callbackFeedback {
	switch action.verb {
	case callbackVerbOpen:
		return callbackNoAckAfterRender(c.renderResetPrompt(ctx, telegramUserID, editMsgID, lang, action.origin))
	case callbackVerbPick:
		if action.resetAction != "" {
			return callbackNoAckAfterRender(c.renderResetConfirm(ctx, telegramUserID, editMsgID, lang, action.origin, action.scope, action.resetAction))
		}
		return callbackNoAckAfterRender(c.renderResetPickedScope(ctx, telegramUserID, editMsgID, lang, action.origin, action.scope))
	case callbackVerbBack:
		return callbackNoAckAfterRender(c.handleResetBack(ctx, telegramUserID, editMsgID, lang, action.origin))
	case callbackVerbMenu:
		return callbackNoAckAfterRender(c.handleResetBackToMenu(ctx, telegramUserID, editMsgID, lang, action.origin))
	case callbackVerbCancel:
		return callbackNoAckAfterRender(c.handleResetCancel(ctx, telegramUserID, editMsgID, lang))
	case callbackVerbExecute:
		return callbackNoAckAfterRender(c.executeReset(ctx, telegramUserID, editMsgID, lang, action.scope, action.resetAction))
	case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect:
		c.log().Warn("unsupported reset callback verb", "telegram_user_id", telegramUserID, "verb", action.verb)
		return noCallbackFeedback()
	default:
		c.log().Warn("unsupported reset callback verb", "telegram_user_id", telegramUserID, "verb", action.verb)
		return noCallbackFeedback()
	}
}

func (c *Bot) renderResetPrompt(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin) string {
	scopes, err := c.reset.LoadScopes(ctx, telegramUserID)
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
		return c.renderResetPickedScope(ctx, telegramUserID, editMsgID, lang, origin, resetScopeViewer)
	}
	return c.renderResetPickedScope(ctx, telegramUserID, editMsgID, lang, origin, resetScopeCreator)
}

func (c *Bot) renderResetPickedScope(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin, scope resetScope) string {
	scopes, err := c.reset.LoadScopes(ctx, telegramUserID)
	if err != nil {
		view := buildResetErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	if c.scopeNeedsCreatorAction(ctx, telegramUserID, scope, scopes) {
		view := c.buildResetCreatorActionView(ctx, telegramUserID, lang, scopes, origin, scope)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return ""
	}
	return c.renderResetConfirm(ctx, telegramUserID, editMsgID, lang, origin, scope, core.CreatorResetKeepMembers)
}

func (c *Bot) renderResetConfirm(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin, scope resetScope, action core.CreatorResetGroupAction) string {
	scopes, err := c.reset.LoadScopes(ctx, telegramUserID)
	if err != nil {
		view := buildResetErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}

	view := c.buildResetConfirmView(ctx, telegramUserID, lang, scopes, scope, action)
	if view.text == "" {
		emptyView := buildResetEmptyView(lang)
		c.reply(ctx, telegramUserID, editMsgID, emptyView.text, &emptyView.opts)
		return ""
	}
	replyView := c.buildResetConfirmReply(ctx, telegramUserID, lang, scopes, view, origin, scope, action)
	c.reply(ctx, telegramUserID, editMsgID, replyView.text, &replyView.opts)
	return ""
}

func (c *Bot) handleResetBack(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin) string {
	scopes, err := c.reset.LoadScopes(ctx, telegramUserID)
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

func (c *Bot) handleResetBackToMenu(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin) string {
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

func (c *Bot) handleResetCancel(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	view := buildTextView(lang, msgResetExitHTML)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) executeReset(ctx context.Context, telegramUserID int64, editMsgID int, lang string, scope resetScope, action core.CreatorResetGroupAction) string {
	switch scope {
	case resetScopeViewer:
		return c.handleResetViewerCommand(ctx, telegramUserID, editMsgID, lang)
	case resetScopeCreator:
		return c.handleResetCreatorCommand(ctx, telegramUserID, editMsgID, lang, action)
	case resetScopeBoth:
		return c.handleResetBothCommand(ctx, telegramUserID, editMsgID, lang, action)
	default:
		c.log().Warn("unsupported reset execute scope", "telegram_user_id", telegramUserID, "scope", scope)
		return ""
	}
}

func (c *Bot) handleResetViewerCommand(ctx context.Context, telegramUserID int64, editMsgID int, lang string) string {
	return c.executeResetScope(ctx, telegramUserID, editMsgID, lang, usecase.ResetScopeViewer, "")
}

func (c *Bot) handleResetCreatorCommand(ctx context.Context, telegramUserID int64, editMsgID int, lang string, action core.CreatorResetGroupAction) string {
	return c.executeResetScope(ctx, telegramUserID, editMsgID, lang, usecase.ResetScopeCreator, action)
}

func (c *Bot) handleResetBothCommand(ctx context.Context, telegramUserID int64, editMsgID int, lang string, action core.CreatorResetGroupAction) string {
	return c.executeResetScope(ctx, telegramUserID, editMsgID, lang, usecase.ResetScopeBoth, action)
}

func (c *Bot) executeResetScope(ctx context.Context, telegramUserID int64, editMsgID int, lang string, scope usecase.ResetScope, action core.CreatorResetGroupAction) string {
	res, err := c.reset.Execute(ctx, telegramUserID, scope, action)
	if err != nil {
		view := buildResetErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	if res.Empty {
		view := buildResetEmptyView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return ""
	}
	view := buildResetExecutionView(lang, res)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func resetChooseScopeText(lang string, scopes core.ScopeState) string {
	return fmt.Sprintf(
		i18n.Translate(lang, msgResetChooseScopeHTML),
		html.EscapeString(scopes.Identity.TwitchLogin),
		html.EscapeString(scopes.Creator.TwitchLogin),
	)
}

func (c *Bot) buildResetConfirmView(ctx context.Context, telegramUserID int64, lang string, scopes core.ScopeState, scope resetScope, action core.CreatorResetGroupAction) resetConfirmView {
	switch scope {
	case resetScopeViewer:
		if !scopes.HasIdentity {
			return resetConfirmView{}
		}
		return resetConfirmView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgResetConfirmViewerHTML),
				html.EscapeString(scopes.Identity.TwitchLogin),
				c.resetViewerGroupCount(ctx, telegramUserID),
			),
		}
	case resetScopeCreator:
		if !scopes.HasCreator {
			return resetConfirmView{}
		}
		return resetConfirmView{text: fmt.Sprintf(
			i18n.Translate(lang, msgResetConfirmCreatorHTML),
			html.EscapeString(scopes.Creator.TwitchLogin),
			1,
			c.resetCreatorGroupCount(ctx, telegramUserID),
			resetActionSummaryText(lang, action),
		)}
	case resetScopeBoth:
		if !scopes.HasIdentity && !scopes.HasCreator {
			return resetConfirmView{}
		}
		viewerName := "-"
		if scopes.HasIdentity {
			viewerName = html.EscapeString(scopes.Identity.TwitchLogin)
		}
		creatorName := "-"
		creatorCount := 0
		if scopes.HasCreator {
			creatorName = html.EscapeString(scopes.Creator.TwitchLogin)
			creatorCount = 1
		}
		return resetConfirmView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgResetConfirmBothHTML),
				viewerName,
				creatorName,
				creatorCount,
				c.resetViewerGroupCount(ctx, telegramUserID),
				c.resetCreatorGroupCount(ctx, telegramUserID),
				resetActionSummaryText(lang, action),
			),
		}
	default:
		c.log().Warn("unsupported reset scope", "telegram_user_id", telegramUserID, "scope", scope)
		return resetConfirmView{}
	}
}

func (c *Bot) resetViewerGroupCount(ctx context.Context, telegramUserID int64) int {
	groupCount, err := c.reset.CountViewerGroups(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("count viewer groups failed", "telegram_user_id", telegramUserID, "error", err)
		return 0
	}
	return groupCount
}

func (c *Bot) resetCreatorGroupCount(ctx context.Context, telegramUserID int64) int {
	groupCount, err := c.reset.CountCreatorGroups(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("count creator groups failed", "telegram_user_id", telegramUserID, "error", err)
		return 0
	}
	return groupCount
}

func buildResetPromptView(lang string, scopes core.ScopeState, origin resetOrigin) (sharedView, bool) {
	if !scopes.HasIdentity && !scopes.HasCreator {
		return buildResetEmptyView(lang), true
	}
	if !scopes.HasIdentity || !scopes.HasCreator {
		return sharedView{}, false
	}
	return sharedView{
		text: resetChooseScopeText(lang, scopes),
		opts: client.MessageOptions{
			Markup: ui.ResetScopePickerMarkup(
				lang,
				resetPickCallback(origin, resetScopeViewer),
				resetPickCallback(origin, resetScopeCreator),
				resetPickCallback(origin, resetScopeBoth),
				resetPromptBackCallback(origin),
			),
		},
	}, true
}

func (c *Bot) buildResetConfirmReply(ctx context.Context, telegramUserID int64, lang string, scopes core.ScopeState, view resetConfirmView, origin resetOrigin, scope resetScope, action core.CreatorResetGroupAction) sharedView {
	confirmCallback := resetExecuteCallback(origin, scope)
	backCallback := resetBackCallback(origin)
	if c.scopeNeedsCreatorAction(ctx, telegramUserID, scope, scopes) {
		confirmCallback = resetExecuteWithActionCallback(origin, scope, action)
		backCallback = resetPickCallback(origin, scope)
	}
	return sharedView{
		text: view.text,
		opts: client.MessageOptions{
			Markup: ui.ResetConfirmMarkup(lang, confirmCallback, backCallback),
		},
	}
}

func buildResetErrorView(lang string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgErrReset),
		opts: client.MessageOptions{Markup: viewerMainMenuMarkup(lang)},
	}
}

func buildResetEmptyView(lang string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgResetNothingHTML),
		opts: client.MessageOptions{},
	}
}

func buildResetExecutionView(lang string, res usecase.ResetResult) sharedView {
	return sharedView{
		text: renderResetExecutionResult(lang, res),
		opts: client.MessageOptions{},
	}
}

func (c *Bot) scopeNeedsCreatorAction(ctx context.Context, telegramUserID int64, scope resetScope, scopes core.ScopeState) bool {
	if scope != resetScopeCreator && scope != resetScopeBoth {
		return false
	}
	if !scopes.HasCreator {
		return false
	}
	return c.resetCreatorGroupCount(ctx, telegramUserID) > 0
}

func (c *Bot) buildResetCreatorActionView(ctx context.Context, telegramUserID int64, lang string, scopes core.ScopeState, origin resetOrigin, scope resetScope) sharedView {
	groupCount := c.resetCreatorGroupCount(ctx, telegramUserID)
	textKey := msgResetChooseCreatorActionCreator
	args := []any{
		html.EscapeString(scopes.Creator.TwitchLogin),
		groupCount,
	}
	if scope == resetScopeBoth {
		textKey = msgResetChooseCreatorActionBoth
		viewerName := "-"
		if scopes.HasIdentity {
			viewerName = html.EscapeString(scopes.Identity.TwitchLogin)
		}
		args = []any{
			viewerName,
			html.EscapeString(scopes.Creator.TwitchLogin),
			groupCount,
		}
	}
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, textKey), args...),
		opts: client.MessageOptions{
			Markup: tu.InlineKeyboard(
				tu.InlineKeyboardRow(ui.CallbackButton(i18n.Translate(lang, btnResetKeepMembers), resetActionPickCallback(origin, scope, core.CreatorResetKeepMembers))),
				tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnResetKickTrackedMembers), resetActionPickCallback(origin, scope, core.CreatorResetKickTrackedMembers), "5258318620722733379").WithStyle("danger")),
				tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), resetBackCallback(origin))),
			),
		},
	}
}

func resetPromptBackCallback(origin resetOrigin) string {
	switch origin {
	case resetOriginViewer, resetOriginCreator:
		return resetMenuCallback(origin)
	case resetOriginCommand:
		return resetCancelCallback(origin)
	}
	return ""
}

func renderResetExecutionResult(lang string, res usecase.ResetResult) string {
	switch res.Scope {
	case usecase.ResetScopeViewer:
		return fmt.Sprintf(i18n.Translate(lang, msgResetDoneViewerHTML), html.EscapeString(res.ViewerLogin), res.GroupCount)
	case usecase.ResetScopeCreator:
		return fmt.Sprintf(
			i18n.Translate(lang, msgResetDoneCreatorHTML),
			html.EscapeString(strings.Join(res.DeletedNames, ", ")),
			res.DeletedCount,
			res.CreatorCleanup.ManagedGroupCount,
			renderResetCreatorCleanupResult(lang, res.CreatorCleanup),
		)
	case usecase.ResetScopeBoth:
		viewerName := "-"
		if res.ViewerLogin != "" {
			viewerName = html.EscapeString(res.ViewerLogin)
		}
		return fmt.Sprintf(
			i18n.Translate(lang, msgResetDoneBothHTML),
			viewerName,
			res.GroupCount,
			html.EscapeString(strings.Join(res.DeletedNames, ", ")),
			res.DeletedCount,
			res.CreatorCleanup.ManagedGroupCount,
			renderResetCreatorCleanupResult(lang, res.CreatorCleanup),
		)
	default:
		return i18n.Translate(lang, msgErrReset)
	}
}

func resetActionSummaryText(lang string, action core.CreatorResetGroupAction) string {
	if action == core.CreatorResetKickTrackedMembers {
		return i18n.Translate(lang, msgResetActionKickLine)
	}
	return i18n.Translate(lang, msgResetActionKeepLine)
}

func renderResetCreatorCleanupResult(lang string, cleanup core.CreatorGroupCleanupSummary) string {
	lines := []string{
		fmt.Sprintf(i18n.Translate(lang, msgResetCreatorGroupsLine), cleanup.ManagedGroupCount),
	}
	if cleanup.Action == core.CreatorResetKickTrackedMembers {
		lines = append(lines, fmt.Sprintf(i18n.Translate(lang, msgResetCreatorKickTargetsLine), cleanup.TargetedMembershipCount))
		if cleanup.QueueFailed {
			lines = append(lines, i18n.Translate(lang, "reset_creator_cleanup_queue_failed"))
		} else if cleanup.Queued {
			lines = append(lines, i18n.Translate(lang, "reset_creator_cleanup_queued"))
		}
	} else {
		lines = append(lines, i18n.Translate(lang, msgResetActionKeepLine))
	}
	return strings.Join(lines, "\n")
}
