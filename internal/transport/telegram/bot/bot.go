package bot

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	telegramui "imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	tghandler "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

const msgCbRefreshed = "cb_refreshed"

const (
	telegramChatTypePrivate = "private"
	telegramChatTypeGroup   = "group"
	telegramChatTypeOther   = "other"
)

type telegramCommandResponseKey struct{}
type telegramCallbackResponseKey struct{}
type callbackFeedback struct {
	ackText   string
	showAlert bool
}

type telegramCommandResponseState struct {
	command  string
	chatType string
	started  time.Time
	result   string
	once     sync.Once
}

type telegramCallbackResponseState struct {
	domain  string
	verb    string
	started time.Time
	result  string
	once    sync.Once
}

func withTelegramCommandResponse(ctx context.Context, command, chatType string, started time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, telegramCommandResponseKey{}, &telegramCommandResponseState{
		command:  strings.TrimSpace(command),
		chatType: normalizeTelegramChatType(chatType),
		started:  started.UTC(),
	})
}

func withTelegramCallbackResponse(ctx context.Context, domain, verb string, started time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, telegramCallbackResponseKey{}, &telegramCallbackResponseState{
		domain:  strings.TrimSpace(domain),
		verb:    strings.TrimSpace(verb),
		started: started.UTC(),
	})
}

func noCallbackFeedback() callbackFeedback {
	return callbackFeedback{}
}

func callbackAlert(text string) callbackFeedback {
	return callbackFeedback{ackText: strings.TrimSpace(text), showAlert: true}
}

func callbackAck(text string) callbackFeedback {
	return callbackFeedback{ackText: strings.TrimSpace(text), showAlert: false}
}

func callbackNoAckAfterRender(_ string) callbackFeedback {
	return noCallbackFeedback()
}

func (c *Bot) oauthStartURL(state string) string {
	return c.cfg.PublicBaseURL + "/auth/start/" + url.PathEscape(state)
}

// sendMsg sends a Telegram message and returns its message ID, or 0 on failure.
func (c *Bot) sendMsg(ctx context.Context, chatID int64, text string, opts *client.MessageOptions) int {
	if c == nil || c.telegramClient == nil {
		return 0
	}
	messageID := c.telegramClient.Send(ctx, chatID, text, opts)
	if messageID != 0 {
		c.observeTelegramCommandResponse(ctx, "ok")
		c.observeTelegramCallbackResponse(ctx, "ok")
		return messageID
	}
	c.observeTelegramCommandResponse(ctx, "send_failed")
	c.observeTelegramCallbackResponse(ctx, "send_failed")
	return messageID
}

func (c *Bot) reply(ctx context.Context, chatID int64, messageID int, text string, opts *client.MessageOptions) {
	if c == nil || c.telegramClient == nil {
		return
	}
	replyID := c.telegramClient.Reply(ctx, chatID, messageID, text, opts)
	if replyID != 0 {
		c.observeTelegramCommandResponse(ctx, "ok")
		c.observeTelegramCallbackResponse(ctx, "ok")
		return
	}
	c.observeTelegramCommandResponse(ctx, "send_failed")
	c.observeTelegramCallbackResponse(ctx, "send_failed")
}

func (c *Bot) deleteMessage(ctx context.Context, chatID int64, messageID int) {
	if c == nil || c.telegramClient == nil {
		return
	}
	c.telegramClient.Delete(ctx, chatID, messageID)
}

func (c *Bot) createInviteLink(ctx context.Context, groupChatID int64, telegramUserID int64, name string) (string, error) {
	if c == nil || c.telegramGroups == nil {
		return "", errTelegramBotNotConfigured
	}
	link, err := c.telegramGroups.CreateInviteLink(ctx, groupChatID, telegramUserID, name)
	if err != nil {
		return "", fmt.Errorf("create invite link from group ops: %w", err)
	}
	return link, nil
}

func (c *Bot) botUsername(ctx context.Context) string {
	if c == nil || c.tg == nil {
		return ""
	}
	c.botUsernameMu.Lock()
	defer c.botUsernameMu.Unlock()
	if c.botUsernameCached != "" {
		return c.botUsernameCached
	}
	me, err := c.tg.GetMe(ctx)
	if err != nil {
		c.log().Warn("GetMe failed while resolving bot username", "error", err)
		return ""
	}
	c.botUsernameCached = strings.TrimSpace(me.Username)
	return c.botUsernameCached
}

func (c *Bot) kickDisplacedUser(ctx context.Context, telegramUserID int64) {
	if c == nil || c.telegramGroups == nil {
		return
	}
	c.telegramGroups.KickDisplacedUser(ctx, telegramUserID)
}

func (c *Bot) isGroupMember(ctx context.Context, groupChatID, telegramUserID int64) bool {
	if c == nil || c.telegramGroups == nil {
		return false
	}
	return c.telegramGroups.IsGroupMember(ctx, groupChatID, telegramUserID)
}

// KickFromGroup removes a Telegram user from a managed group for a specific workflow reason.
func (c *Bot) KickFromGroup(ctx context.Context, groupChatID int64, telegramUserID int64, reason core.KickReason) error {
	if c == nil || c.telegramGroups == nil {
		return nil
	}
	if err := c.telegramGroups.KickFromGroup(ctx, groupChatID, telegramUserID, reason); err != nil {
		return fmt.Errorf("kick from group via group ops: %w", err)
	}
	return nil
}

func (c *Bot) trackTelegramActiveUser(ctx context.Context, telegramUserID int64) {
	if c == nil || c.store == nil || telegramUserID == 0 {
		return
	}
	if err := c.store.TrackTelegramActiveUser(ctx, telegramUserID, time.Now().UTC()); err != nil {
		c.log().Warn("track telegram active user failed", "telegram_user_id", telegramUserID, "error", err)
	}
}

func (c *Bot) recordTelegramCommand(ctx context.Context, telegramUserID int64, command, chatType string) {
	c.trackTelegramActiveUser(ctx, telegramUserID)
	if c == nil || c.events == nil {
		return
	}
	c.events.Emit(ctx, events.Event{
		Name: events.NameTelegramCommand,
		Fields: map[string]string{
			"command":   strings.TrimSpace(command),
			"chat_type": normalizeTelegramChatType(chatType),
		},
	})
}

func (c *Bot) observeTelegramCommandResponse(ctx context.Context, result string) {
	if c == nil || c.events == nil || ctx == nil {
		return
	}
	state, ok := ctx.Value(telegramCommandResponseKey{}).(*telegramCommandResponseState)
	if !ok || state == nil || state.started.IsZero() {
		return
	}
	state.once.Do(func() {
		result = strings.TrimSpace(result)
		if (result == "" || result == "ok") && state.result != "" {
			result = state.result
		}
		if result == "" {
			result = "ok"
		}
		c.events.Emit(ctx, events.Event{
			Name:    events.NameTelegramCommandResponse,
			Outcome: result,
			Fields: map[string]string{
				"command":   state.command,
				"chat_type": state.chatType,
			},
			Duration: time.Since(state.started),
		})
	})
}

func setTelegramCommandResponseResult(ctx context.Context, result string) {
	if ctx == nil {
		return
	}
	state, ok := ctx.Value(telegramCommandResponseKey{}).(*telegramCommandResponseState)
	if !ok || state == nil {
		return
	}
	if trimmed := strings.TrimSpace(result); trimmed != "" {
		state.result = trimmed
	}
}

func (c *Bot) recordTelegramCallback(ctx context.Context, telegramUserID int64, action callbackAction) {
	c.trackTelegramActiveUser(ctx, telegramUserID)
	if c == nil || c.events == nil {
		return
	}
	c.events.Emit(ctx, events.Event{
		Name: events.NameTelegramCallback,
		Fields: map[string]string{
			"domain": strings.TrimSpace(string(action.domain)),
			"verb":   strings.TrimSpace(string(action.verb)),
		},
	})
}

func setTelegramCallbackResponseResult(ctx context.Context, result string) {
	if ctx == nil {
		return
	}
	state, ok := ctx.Value(telegramCallbackResponseKey{}).(*telegramCallbackResponseState)
	if !ok || state == nil {
		return
	}
	if trimmed := strings.TrimSpace(result); trimmed != "" {
		state.result = trimmed
	}
}

func (c *Bot) observeTelegramCallbackResponse(ctx context.Context, result string) {
	if c == nil || c.events == nil || ctx == nil {
		return
	}
	state, ok := ctx.Value(telegramCallbackResponseKey{}).(*telegramCallbackResponseState)
	if !ok || state == nil || state.started.IsZero() {
		return
	}
	state.once.Do(func() {
		result = strings.TrimSpace(result)
		if (result == "" || result == "ok") && state.result != "" {
			result = state.result
		}
		if result == "" {
			result = "ok"
		}
		c.events.Emit(ctx, events.Event{
			Name:    events.NameTelegramCallbackResponse,
			Outcome: result,
			Fields: map[string]string{
				"domain": state.domain,
				"verb":   state.verb,
			},
			Duration: time.Since(state.started),
		})
	})
}

func normalizeTelegramChatType(chatType string) string {
	switch chatType {
	case telego.ChatTypePrivate:
		return telegramChatTypePrivate
	case telego.ChatTypeGroup, telego.ChatTypeSupergroup:
		return telegramChatTypeGroup
	default:
		return telegramChatTypeOther
	}
}

func renderJoinButtons(targets core.JoinTargets, lang string) [][]telego.InlineKeyboardButton {
	rows := make([][]telego.InlineKeyboardButton, 0, len(targets.JoinLinks))
	for _, link := range targets.JoinLinks {
		btnText := link.CreatorName + " - " + link.GroupName
		rows = append(rows, tu.InlineKeyboardRow(telegramui.LinkButton(fmt.Sprintf(i18n.Translate(lang, btnJoin), btnText), link.InviteLink)))
	}
	return rows
}

func (c *Bot) answerCallback(ctx context.Context, callbackID, text string) {
	c.answerCallbackOpts(ctx, callbackID, text, false)
}

func (c *Bot) answerCallbackOpts(ctx context.Context, callbackID, text string, showAlert bool) {
	if c == nil || c.telegramClient == nil {
		return
	}
	c.telegramClient.AnswerCallback(ctx, callbackID, text, showAlert)
}

func viewerMainMenuCallbacks() telegramui.MainMenuCallbacks {
	return telegramui.MainMenuCallbacks{
		Refresh: viewerRefreshCallback(),
		Reset:   resetOpenCallback(resetOriginViewer),
	}
}

func viewerMainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return telegramui.MainMenuMarkup(lang, viewerMainMenuCallbacks())
}

func creatorStatusMenuCallbacks(hasManageGroups, isActive bool, graceActive, blocklistActive bool) telegramui.CreatorMenuCallbacks {
	callbacks := telegramui.CreatorMenuCallbacks{
		Refresh: creatorRefreshCallback(),
		Reset:   resetOpenCallback(resetOriginCreator),
	}
	if hasManageGroups {
		callbacks.ManageGroups = creatorManageGroupsCallback()
	}
	if isActive {
		callbacks.Grace = creatorGraceOpenCallback()
		callbacks.GraceActive = graceActive
		callbacks.Blocklist = creatorBlocklistToggleCallback()
		callbacks.BlocklistActive = blocklistActive
	}
	return callbacks
}

func creatorMainMenuCallbacks() telegramui.CreatorMenuCallbacks {
	return telegramui.CreatorMenuCallbacks{
		Refresh: creatorRefreshCallback(),
		Reset:   resetOpenCallback(resetOriginCreator),
	}
}

func creatorMainMenuMarkup(lang string) *telego.InlineKeyboardMarkup {
	return telegramui.CreatorMainMenuMarkup(lang, creatorMainMenuCallbacks())
}

func (c *Bot) createOAuthState(ctx context.Context, payload core.OAuthStatePayload, ttl time.Duration) (string, error) {
	state, err := NewSecureToken(24)
	if err != nil {
		return "", fmt.Errorf("generate secure token: %w", err)
	}
	if err := c.store.SaveOAuthState(ctx, state, payload, ttl); err != nil {
		return "", fmt.Errorf("save oauth state: %w", err)
	}
	return state, nil
}

func (c *Bot) invalidateOAuthState(ctx context.Context, state string) {
	if state == "" {
		return
	}
	cleanupCtx := context.WithoutCancel(ctx)
	if _, err := c.store.DeleteOAuthState(cleanupCtx, state); err != nil {
		c.log().Warn("deleteOAuthState cleanup failed", "state", state, "error", err)
	}
}

// RegisterTelegramHandlers binds Telegram commands, callbacks, and join-request handlers.
func (c *Bot) RegisterTelegramHandlers() {
	if c.tgHandler == nil {
		return
	}

	privateOnly := func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Chat.Type == telego.ChatTypePrivate && update.Message.From != nil
	}
	groupOnly := func(_ context.Context, update telego.Update) bool {
		return update.Message != nil && update.Message.Chat.Type != telego.ChatTypePrivate && update.Message.From != nil
	}
	trackedCommand := func(command, chatType string, handler func(*tghandler.Context, telego.Message) error) func(*tghandler.Context, telego.Message) error {
		return func(ctx *tghandler.Context, msg telego.Message) error {
			ctx = ctx.WithContext(withTelegramCommandResponse(ctx, command, chatType, time.Now()))
			if msg.From != nil {
				c.recordTelegramCommand(ctx, msg.From.ID, command, chatType)
			}
			err := handler(ctx, msg)
			if err != nil {
				c.observeTelegramCommandResponse(ctx, "error")
				return err
			}
			return nil
		}
	}

	c.tgHandler.HandleMessage(trackedCommand("linkgroup", "group", c.onRegisterGroup), tghandler.CommandEqual("linkgroup"))
	c.tgHandler.HandleMessage(trackedCommand("unlinkgroup", "group", c.onUnregisterCommand), tghandler.And(tghandler.CommandEqual("unlinkgroup"), groupOnly))
	c.tgHandler.HandleMessage(trackedCommand("start", "private", c.onStartCommand), tghandler.And(tghandler.CommandEqual("start"), privateOnly))
	c.tgHandler.HandleMessage(trackedCommand("creator", "private", c.onCreatorCommand), tghandler.And(tghandler.CommandEqual("creator"), privateOnly))
	c.tgHandler.HandleMessage(trackedCommand("reset", "private", c.onResetCommand), tghandler.And(tghandler.CommandEqual("reset"), privateOnly))
	c.tgHandler.HandleMessage(trackedCommand("info", "private", c.onInfoCommand), tghandler.And(tghandler.CommandEqual("info"), privateOnly))
	c.tgHandler.HandleCallbackQuery(func(ctx *tghandler.Context, query telego.CallbackQuery) error {
		c.onCallbackQuery(ctx, query)
		return nil
	}, tghandler.AnyCallbackQuery())
	c.tgHandler.HandleChatJoinRequest(c.onChatJoinRequest)
	c.tgHandler.HandleChatMemberUpdated(c.onChatMemberUpdated)
	c.tgHandler.HandleMyChatMemberUpdated(c.onMyChatMemberUpdated)
	c.tgHandler.HandleMessage(c.onGroupMessage, tghandler.And(tghandler.AnyMessage(), groupOnly))
	c.tgHandler.HandleMessage(c.onUnknownMessage, tghandler.And(tghandler.AnyMessage(), privateOnly))
}

func (c *Bot) onCallbackQuery(ctx context.Context, q telego.CallbackQuery) {
	c.trackTelegramActiveUser(ctx, q.From.ID)
	lang := i18n.NormalizeLanguage(q.From.LanguageCode)
	exec := callbackExecution{
		userID: q.From.ID,
		lang:   lang,
	}
	if q.Message != nil {
		exec.editMsgID = q.Message.GetMessageID()
		exec.editChatID = q.Message.GetChat().ID
		exec.editChatTitle = q.Message.GetChat().Title
	}

	action, ok := parseCallbackAction(q.Data)
	if !ok {
		c.log().Warn("ignore unknown callback data", "telegram_user_id", q.From.ID, "data", q.Data)
		c.answerCallback(ctx, q.ID, "")
		return
	}

	exec.action = action
	ctx = withTelegramCallbackResponse(ctx, string(action.domain), string(action.verb), time.Now())
	c.recordTelegramCallback(ctx, q.From.ID, action)
	feedback := c.dispatchCallbackAction(ctx, exec)
	if feedback.ackText == "" && action.verb == callbackVerbRefresh {
		feedback = callbackAck(i18n.Translate(exec.lang, msgCbRefreshed))
	}
	if feedback.ackText != "" {
		c.log().Info("sending callback acknowledgement", "telegram_user_id", q.From.ID, "callback_data", q.Data, "domain", action.domain, "verb", action.verb, "ack_len", utf8.RuneCountInString(feedback.ackText), "show_alert", feedback.showAlert)
		c.answerCallbackOpts(ctx, q.ID, feedback.ackText, feedback.showAlert)
		c.observeTelegramCallbackResponse(ctx, "ok")
		return
	}
	c.answerCallback(ctx, q.ID, "")
	c.observeTelegramCallbackResponse(ctx, "ok")
}

type callbackExecution struct {
	userID        int64
	editChatID    int64
	editChatTitle string
	editMsgID     int
	lang          string
	action        callbackAction
}

func (c *Bot) dispatchCallbackAction(ctx context.Context, exec callbackExecution) callbackFeedback {
	switch exec.action.domain {
	case callbackDomainViewer:
		return callbackNoAckAfterRender(c.handleViewerStart(ctx, exec.userID, exec.editMsgID, exec.lang))
	case callbackDomainCreator:
		return c.handleCreatorCallback(ctx, exec.userID, exec.editMsgID, exec.lang, exec.action)
	case callbackDomainGroup:
		return c.handleGroupCallback(ctx, exec.userID, exec.editChatID, exec.editChatTitle, exec.editMsgID, exec.lang, exec.action)
	case callbackDomainReset:
		return c.handleResetAction(ctx, exec.userID, exec.editMsgID, exec.lang, exec.action)
	}
	c.log().Warn("unsupported callback action", "telegram_user_id", exec.userID, "data", exec.action.String())
	return noCallbackFeedback()
}

func (c *Bot) onInfoCommand(ctx *tghandler.Context, msg telego.Message) error {
	lang := i18n.NormalizeLanguage(msg.From.LanguageCode)
	view := buildInfoView(lang)
	c.sendMsg(ctx, msg.Chat.ID, view.text, &view.opts)
	return nil
}

func (c *Bot) onUnknownMessage(ctx *tghandler.Context, message telego.Message) error {
	lang := i18n.NormalizeLanguage(message.From.LanguageCode)
	key := msgCmdHelp
	if message.From != nil {
		var err error
		key, err = c.helpMessageKey(ctx, message.From.ID)
		if err != nil {
			c.log().Warn("Resolve help message key failed", "telegram_user_id", message.From.ID, "error", err)
			key = msgCmdHelp
		}
	}
	view := buildMainMenuTextView(lang, key)
	c.sendMsg(ctx, message.Chat.ID, view.text, &view.opts)
	return nil
}

func (c *Bot) helpMessageKey(ctx context.Context, telegramUserID int64) (string, error) {
	_, hasViewer, err := c.viewerAccess.LoadIdentity(ctx, telegramUserID)
	if err != nil {
		return "", fmt.Errorf("load viewer identity for help message: %w", err)
	}
	_, hasCreator, err := c.creatorStatus.LoadOwnedCreator(ctx, telegramUserID)
	if err != nil {
		return "", fmt.Errorf("load owned creator for help message: %w", err)
	}
	switch {
	case hasViewer && hasCreator:
		return msgCmdHelpBoth, nil
	case hasCreator:
		return msgCmdHelpCreator, nil
	case hasViewer:
		return msgCmdHelpViewer, nil
	default:
		return msgCmdHelp, nil
	}
}

func (c *Bot) onChatJoinRequest(ctx *tghandler.Context, req telego.ChatJoinRequest) error {
	if req.InviteLink == nil || !strings.HasPrefix(req.InviteLink.Name, "imsub-") {
		return nil
	}

	parts := strings.SplitN(req.InviteLink.Name, "-", 3)
	if len(parts) < 3 {
		return nil
	}
	linkUserID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || linkUserID != req.From.ID {
		c.log().Info("join request denied", "link_user", parts[1], "requester_id", req.From.ID, "chat_id", req.Chat.ID)
		if waitErr := c.tgLimiter.Wait(ctx, req.Chat.ID); waitErr != nil {
			c.log().Warn("Decline join request rate limit wait failed", "error", waitErr)
			return nil
		}
		if err := c.tg.DeclineChatJoinRequest(ctx, &telego.DeclineChatJoinRequestParams{
			ChatID: tu.ID(req.Chat.ID),
			UserID: req.From.ID,
		}); err != nil {
			c.log().Warn("Decline join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
		}
		return nil
	}

	if waitErr := c.tgLimiter.Wait(ctx, req.Chat.ID); waitErr != nil {
		c.log().Warn("Approve join request rate limit wait failed", "error", waitErr)
		return nil
	}
	if c.shouldDeclineJoinRequest(ctx, req.Chat.ID, req.From.ID) {
		if err := c.tg.DeclineChatJoinRequest(ctx, &telego.DeclineChatJoinRequestParams{
			ChatID: tu.ID(req.Chat.ID),
			UserID: req.From.ID,
		}); err != nil {
			c.log().Warn("Decline blocked join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
		}
		return nil
	}
	err = c.tg.ApproveChatJoinRequest(ctx, &telego.ApproveChatJoinRequestParams{
		ChatID: tu.ID(req.Chat.ID),
		UserID: req.From.ID,
	})
	if err != nil {
		c.log().Warn("Approve join request failed", "user_id", req.From.ID, "chat_id", req.Chat.ID, "error", err)
	}
	return nil
}

func (c *Bot) shouldDeclineJoinRequest(ctx context.Context, chatID, telegramUserID int64) bool {
	if c == nil || c.store == nil {
		return false
	}
	group, ok, err := c.store.ManagedGroupByChatID(ctx, chatID)
	if err != nil {
		c.log().Warn("ManagedGroupByChatID for join request failed", "chat_id", chatID, "error", err)
		return false
	}
	if !ok {
		return false
	}
	creator, creatorFound, err := c.store.Creator(ctx, group.CreatorID)
	if err != nil {
		c.log().Warn("Creator for join request failed", "creator_id", group.CreatorID, "error", err)
		return false
	}
	if !creatorFound || !creator.BlocklistSyncEnabled {
		return false
	}
	identity, found, err := c.store.UserIdentity(ctx, telegramUserID)
	if err != nil {
		c.log().Warn("UserIdentity for join request failed", "telegram_user_id", telegramUserID, "error", err)
		return false
	}
	if !found {
		return false
	}
	blocked, err := c.store.IsCreatorBlocked(ctx, group.CreatorID, identity.TwitchUserID)
	if err != nil {
		c.log().Warn("IsCreatorBlocked for join request failed", "creator_id", group.CreatorID, "twitch_user_id", identity.TwitchUserID, "error", err)
		return false
	}
	return blocked
}

const (
	resultSaveFailed          = "save_failed"
	resultStoreFailed         = "store_failed"
	resultTokenExchangeFailed = "token_exchange_failed"
	resultUserInfoFailed      = "userinfo_failed"
	resultLoadStatusFailed    = "load_status_failed"
	resultScopeMissing        = "scope_missing"
	resultSuccess             = "success"
)

var errReconnectNotificationSend = errors.New("send reconnect-required notification")
var errSubscriptionStartSend = errors.New("send subscription start dm")

// HandleSubscriptionStart proactively sends fresh invites after a Twitch subscription starts.
func (c *Bot) HandleSubscriptionStart(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, _ string) error {
	if c.viewerAccess == nil || c.store == nil {
		return nil
	}
	if c.subscriptionEnd != nil {
		if err := c.subscriptionEnd.CancelGrace(ctx, broadcasterID, twitchUserID); err != nil {
			c.log().Warn("cancel subscription-end grace failed", "broadcaster_id", broadcasterID, "twitch_user_id", twitchUserID, "error", err)
		}
	}
	telegramUserID, found, err := c.store.ResolveTelegramUserIDByTwitch(ctx, twitchUserID)
	if err != nil {
		return fmt.Errorf("resolve telegram user by twitch: %w", err)
	}
	if !found {
		c.log().Debug("skip subscription start dm for unlinked twitch user", "broadcaster_id", broadcasterID, "twitch_user_id", twitchUserID)
		return nil
	}
	if c.godAccess != nil && c.godAccess.IsGodTelegramUser(telegramUserID) {
		return nil
	}
	access, err := c.viewerAccess.LoadAccessForCreator(ctx, broadcasterID, telegramUserID)
	if err != nil {
		return fmt.Errorf("load viewer access for creator: %w", err)
	}
	if !access.HasIdentity || len(access.Targets.JoinLinks) == 0 {
		return nil
	}

	lang := "en"
	if access.Identity.Language != "" {
		lang = i18n.NormalizeLanguage(access.Identity.Language)
	}
	creatorName := broadcasterLogin
	if creatorName == "" && len(access.Targets.ActiveCreatorNames) > 0 {
		creatorName = access.Targets.ActiveCreatorNames[0]
	}
	view := buildSubscriptionStartView(lang, creatorName, access.Targets)
	if messageID := c.sendMsg(ctx, telegramUserID, view.text, &view.opts); messageID == 0 {
		return errSubscriptionStartSend
	}
	return nil
}

// HandleSubscriptionEnd revokes Telegram group access after a Twitch subscription ends.
func (c *Bot) HandleSubscriptionEnd(ctx context.Context, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin string) error {
	res, err := c.subscriptionEnd.Prepare(ctx, broadcasterID, broadcasterLogin, twitchUserID, twitchLogin)
	if err != nil {
		c.log().Warn("process subscription end failed", "error", err)
		return fmt.Errorf("prepare subscription end: %w", err)
	}
	if !res.Prepared.Found {
		return nil
	}
	if res.Prepared.Mode == core.SubscriptionEndModeGrace {
		view := buildSubscriptionGraceStartView(res.Prepared.Language, res.Prepared.BroadcasterLogin, res.Prepared.GraceUntil)
		c.sendMsg(ctx, res.Prepared.TelegramUserID, view.text, &view.opts)
		return nil
	}

	for _, groupChatID := range res.Prepared.GroupChatIDs {
		if err := c.KickFromGroup(ctx, groupChatID, res.Prepared.TelegramUserID, core.KickReasonSubscriptionEnd); err != nil {
			c.log().Warn("kickFromGroup failed", "telegram_user_id", res.Prepared.TelegramUserID, "group_chat_id", groupChatID, "error", err)
		}
	}

	view := buildSubscriptionEndView(res.Prepared.Language, res.Prepared.BroadcasterLogin)
	c.sendMsg(ctx, res.Prepared.TelegramUserID, view.text, &view.opts)
	return nil
}
