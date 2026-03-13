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
	msgResetChooseScopeViewerHTML      = "reset_choose_scope_viewer_html"
	msgResetChooseScopeCreatorHTML     = "reset_choose_scope_creator_html"
	msgResetChooseCreatorActionCreator = "reset_choose_creator_action_creator_html"
	msgResetChooseCreatorActionBoth    = "reset_choose_creator_action_both_html"
	msgResetConfirmViewerHTML          = "reset_confirm_viewer_html"
	msgResetConfirmCreatorHTML         = "reset_confirm_creator_html"
	msgResetConfirmBothHTML            = "reset_confirm_both_html"
	msgResetExitHTML                   = "reset_exit_html"
	msgResetActionKeepLine             = "reset_action_keep_line"
	msgResetActionKickLine             = "reset_action_kick_line"
	msgResetCreatorKickTargetsLine     = "reset_creator_kick_targets_line"
	msgResetCreatorKickFailuresLine    = "reset_creator_kick_failures_line"
	btnResetViewerData                 = "btn_reset_viewer_data"
	btnResetCreatorData                = "btn_reset_creator_data"
	btnResetAllData                    = "btn_reset_all_data"
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
	case callbackVerbExport:
		return c.exportMyData(ctx, telegramUserID, lang)
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

	view := buildResetPromptView(lang, scopes, origin)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func (c *Bot) renderResetPickedScope(ctx context.Context, telegramUserID int64, editMsgID int, lang string, origin resetOrigin, scope resetScope) string {
	scopes, err := c.reset.LoadScopes(ctx, telegramUserID)
	if err != nil {
		view := buildResetErrorView(lang)
		c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
		return view.text
	}
	if c.scopeNeedsCreatorAction(ctx, telegramUserID, scope, scopes) {
		view := c.buildResetCreatorActionView(lang, origin, scope)
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
	if c.privacy != nil {
		receipt, err := c.privacy.RecordDeletionReceipt(ctx, telegramUserID, res)
		if err != nil {
			c.log().Warn("record deletion receipt failed", "telegram_user_id", telegramUserID, "scope", scope, "error", err)
		} else {
			res.Receipt = receipt
		}
	}
	view := buildResetExecutionView(lang, res)
	c.reply(ctx, telegramUserID, editMsgID, view.text, &view.opts)
	return ""
}

func resetChooseScopeText(lang string) string {
	return i18n.Translate(lang, msgResetChooseScopeHTML)
}

func (c *Bot) buildResetConfirmView(ctx context.Context, telegramUserID int64, lang string, scopes core.ScopeState, scope resetScope, action core.CreatorResetGroupAction) resetConfirmView {
	switch scope {
	case resetScopeViewer:
		if !scopes.HasIdentity {
			return resetConfirmView{}
		}
		viewerGroups := c.resetViewerGroupNames(ctx, telegramUserID)
		return resetConfirmView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgResetConfirmViewerHTML),
				twitchProfileHTML(scopes.Identity.TwitchLogin, scopes.Identity.TwitchDisplayName),
				resetGroupSection(lang, i18n.Translate(lang, "reset_subscriber_groups_title"), viewerGroups),
				resetViewerConsequenceLine(lang, len(viewerGroups)),
			),
		}
	case resetScopeCreator:
		if !scopes.HasCreator {
			return resetConfirmView{}
		}
		creatorGroups := c.resetCreatorGroupNames(ctx, telegramUserID)
		return resetConfirmView{text: fmt.Sprintf(
			i18n.Translate(lang, msgResetConfirmCreatorHTML),
			twitchProfileHTML(scopes.Creator.TwitchLogin, scopes.Creator.TwitchDisplayName),
			resetGroupSection(lang, i18n.Translate(lang, "reset_managed_groups_title"), creatorGroups),
			resetCreatorConsequenceLine(lang, len(creatorGroups)),
			resetCreatorActionSummaryText(lang, action, len(creatorGroups)),
		)}
	case resetScopeBoth:
		if !scopes.HasIdentity && !scopes.HasCreator {
			return resetConfirmView{}
		}
		viewerName := "-"
		if scopes.HasIdentity {
			viewerName = twitchProfileHTML(scopes.Identity.TwitchLogin, scopes.Identity.TwitchDisplayName)
		}
		viewerGroups := c.resetViewerGroupNames(ctx, telegramUserID)
		creatorGroups := c.resetCreatorGroupNames(ctx, telegramUserID)
		creatorName := "-"
		if scopes.HasCreator {
			creatorName = twitchProfileHTML(scopes.Creator.TwitchLogin, scopes.Creator.TwitchDisplayName)
		}
		return resetConfirmView{
			text: fmt.Sprintf(
				i18n.Translate(lang, msgResetConfirmBothHTML),
				viewerName,
				resetGroupSection(lang, i18n.Translate(lang, "reset_subscriber_groups_title"), viewerGroups),
				creatorName,
				resetGroupSection(lang, i18n.Translate(lang, "reset_managed_groups_title"), creatorGroups),
				resetViewerConsequenceLine(lang, len(viewerGroups)),
				resetCreatorConsequenceLine(lang, len(creatorGroups)),
				resetCreatorActionSummaryText(lang, action, len(creatorGroups)),
			),
		}
	default:
		c.log().Warn("unsupported reset scope", "telegram_user_id", telegramUserID, "scope", scope)
		return resetConfirmView{}
	}
}

func (c *Bot) resetViewerGroupNames(ctx context.Context, telegramUserID int64) []string {
	names, err := c.reset.ViewerGroupNames(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("load viewer group names failed", "telegram_user_id", telegramUserID, "error", err)
		return nil
	}
	return names
}

func (c *Bot) resetCreatorGroupNames(ctx context.Context, telegramUserID int64) []string {
	names, err := c.reset.CreatorGroupNames(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("load creator group names failed", "telegram_user_id", telegramUserID, "error", err)
		return nil
	}
	return names
}

func (c *Bot) resetCreatorGroupCount(ctx context.Context, telegramUserID int64) int {
	groupCount, err := c.reset.CountCreatorGroups(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("count creator groups failed", "telegram_user_id", telegramUserID, "error", err)
		return 0
	}
	return groupCount
}

func buildResetPromptView(lang string, scopes core.ScopeState, origin resetOrigin) sharedView {
	if !scopes.HasIdentity && !scopes.HasCreator {
		view := buildResetEmptyView(lang)
		view.opts.Markup = resetPromptMarkup(lang, scopes, origin)
		return view
	}
	text := resetChooseScopeText(lang)
	if scopes.HasIdentity && !scopes.HasCreator {
		text = fmt.Sprintf(i18n.Translate(lang, msgResetChooseScopeViewerHTML), twitchProfileHTML(scopes.Identity.TwitchLogin, scopes.Identity.TwitchDisplayName))
	}
	if !scopes.HasIdentity && scopes.HasCreator {
		text = fmt.Sprintf(i18n.Translate(lang, msgResetChooseScopeCreatorHTML), twitchProfileHTML(scopes.Creator.TwitchLogin, scopes.Creator.TwitchDisplayName))
	}
	return sharedView{
		text: text,
		opts: client.MessageOptions{
			Markup: resetPromptMarkup(lang, scopes, origin),
		},
	}
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

func resetPromptMarkup(lang string, scopes core.ScopeState, origin resetOrigin) *telego.InlineKeyboardMarkup {
	rows := make([][]telego.InlineKeyboardButton, 0, 5)
	switch {
	case scopes.HasIdentity && scopes.HasCreator:
		rows = append(rows,
			tu.InlineKeyboardRow(ui.DeleteButton(i18n.Translate(lang, btnResetViewerData)+": "+twitchAccountLabel(scopes.Identity.TwitchLogin, scopes.Identity.TwitchDisplayName), resetPickCallback(origin, resetScopeViewer))),
			tu.InlineKeyboardRow(ui.DeleteButton(i18n.Translate(lang, btnResetCreatorData)+": "+twitchAccountLabel(scopes.Creator.TwitchLogin, scopes.Creator.TwitchDisplayName), resetPickCallback(origin, resetScopeCreator))),
			tu.InlineKeyboardRow(ui.DeleteButton(i18n.Translate(lang, btnResetAllData), resetPickCallback(origin, resetScopeBoth))),
		)
	case scopes.HasIdentity:
		rows = append(rows, tu.InlineKeyboardRow(ui.DeleteButton(i18n.Translate(lang, btnResetViewerData)+": "+twitchAccountLabel(scopes.Identity.TwitchLogin, scopes.Identity.TwitchDisplayName), resetPickCallback(origin, resetScopeViewer))))
	case scopes.HasCreator:
		rows = append(rows, tu.InlineKeyboardRow(ui.DeleteButton(i18n.Translate(lang, btnResetCreatorData)+": "+twitchAccountLabel(scopes.Creator.TwitchLogin, scopes.Creator.TwitchDisplayName), resetPickCallback(origin, resetScopeCreator))))
	}
	if scopes.HasIdentity || scopes.HasCreator {
		rows = append(rows, tu.InlineKeyboardRow(ui.IconCallbackButton(i18n.Translate(lang, btnExportData), resetExportCallback(origin), exportDataEmojiID)))
	}
	backCallback := resetPromptBackCallback(origin)
	if backCallback != "" {
		rows = append(rows, tu.InlineKeyboardRow(ui.BackButton(i18n.Translate(lang, btnBack), backCallback)))
	}
	return tu.InlineKeyboard(rows...)
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

func (c *Bot) buildResetCreatorActionView(lang string, origin resetOrigin, scope resetScope) sharedView {
	textKey := msgResetChooseCreatorActionCreator
	args := []any{}
	if scope == resetScopeBoth {
		textKey = msgResetChooseCreatorActionBoth
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
	receiptLine := ""
	if res.Receipt.ID != "" {
		receiptLine = "\n\nReceipt ID: <code>" + html.EscapeString(res.Receipt.ID) + "</code>"
	}
	switch res.Scope {
	case usecase.ResetScopeViewer:
		return fmt.Sprintf(i18n.Translate(lang, msgResetDoneViewerHTML), twitchProfileHTML(res.ViewerLogin, res.ViewerDisplayName), renderResetViewerGroups(lang, res.GroupNames)) + receiptLine
	case usecase.ResetScopeCreator:
		return fmt.Sprintf(
			i18n.Translate(lang, msgResetDoneCreatorHTML),
			twitchProfileHTML(res.CreatorLogin, res.CreatorDisplayName),
			renderResetViewerGroups(lang, res.CreatorCleanup.GroupNames),
			renderResetCreatorCleanupResult(lang, res.CreatorCleanup),
		) + receiptLine
	case usecase.ResetScopeBoth:
		viewerName := "-"
		if res.ViewerLogin != "" {
			viewerName = twitchProfileHTML(res.ViewerLogin, res.ViewerDisplayName)
		}
		return fmt.Sprintf(
			i18n.Translate(lang, msgResetDoneBothHTML),
			viewerName,
			renderResetViewerGroups(lang, res.GroupNames),
			twitchProfileHTML(res.CreatorLogin, res.CreatorDisplayName),
			renderResetViewerGroups(lang, res.CreatorCleanup.GroupNames),
			renderResetCreatorCleanupResult(lang, res.CreatorCleanup),
		) + receiptLine
	default:
		return i18n.Translate(lang, msgErrReset)
	}
}

func twitchProfileHTML(login, displayName string) string {
	return ui.TwitchProfileHTML(login, displayName)
}

func twitchAccountLabel(login, displayName string) string {
	name := strings.TrimSpace(displayName)
	if name != "" {
		return name
	}
	return strings.TrimSpace(login)
}

func resetCreatorActionSummaryText(lang string, action core.CreatorResetGroupAction, groupCount int) string {
	if groupCount == 0 {
		return ""
	}
	if action == core.CreatorResetKickTrackedMembers {
		return i18n.Translate(lang, msgResetActionKickLine)
	}
	return i18n.Translate(lang, msgResetActionKeepLine)
}

func renderResetCreatorCleanupResult(lang string, cleanup core.CreatorGroupCleanupSummary) string {
	lines := []string{}
	if cleanup.Action == core.CreatorResetKickTrackedMembers {
		if cleanup.QueueFailed {
			lines = append(lines, i18n.Translate(lang, "reset_creator_cleanup_queue_failed"))
		} else if cleanup.Queued || cleanup.TargetedMembershipCount > 0 {
			lines = append(lines, i18n.Translate(lang, msgResetCreatorKickTargetsLine))
		}
	} else {
		lines = append(lines, i18n.Translate(lang, msgResetActionKeepLine))
	}
	return strings.Join(compactNonEmpty(lines), "\n")
}

func compactNonEmpty(lines []string) []string {
	out := lines[:0]
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func renderResetViewerGroups(lang string, names []string) string {
	if len(names) == 0 {
		return i18n.Translate(lang, "reset_no_groups_line")
	}
	items := make([]string, 0, len(names))
	for _, name := range names {
		items = append(items, "• "+html.EscapeString(name))
	}
	return strings.Join(items, "\n")
}

func resetViewerConsequenceLine(lang string, groupCount int) string {
	if groupCount == 0 {
		return "\n" + i18n.Translate(lang, "reset_viewer_no_groups_line")
	}
	return "\n" + i18n.Translate(lang, "reset_viewer_remove_groups_line")
}

func resetCreatorConsequenceLine(lang string, groupCount int) string {
	if groupCount == 0 {
		return "\n" + i18n.Translate(lang, "reset_creator_no_groups_line")
	}
	return "\n" + strings.Join([]string{
		i18n.Translate(lang, "reset_creator_stop_checks_line"),
		i18n.Translate(lang, "reset_creator_stop_control_line"),
	}, "\n")
}

func resetGroupSection(lang, title string, names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("\n<b>%s:</b>\n%s", title, renderResetViewerGroups(lang, names))
}
