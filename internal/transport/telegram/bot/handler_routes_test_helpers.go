package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/config"
	"imsub/internal/platform/ratelimit"
	telegramclient "imsub/internal/transport/telegram/client"
	telegramgroups "imsub/internal/transport/telegram/groups"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
)

var errUnexpectedTelegramMethod = errors.New("unexpected Telegram method")

type editMessageRequest struct {
	Text        string `json:"text"`
	ReplyMarkup struct {
		InlineKeyboard [][]struct {
			CallbackData string `json:"callback_data"`
			URL          string `json:"url"`
		} `json:"inline_keyboard"`
	} `json:"reply_markup"`
}

type sendMessageRequest struct {
	ChatID      int64  `json:"chat_id"`
	Text        string `json:"text"`
	ReplyMarkup struct {
		InlineKeyboard [][]struct {
			CallbackData string `json:"callback_data"`
			URL          string `json:"url"`
		} `json:"inline_keyboard"`
	} `json:"reply_markup"`
}

type routeTestHarness struct {
	controller *Bot
	bot        *telego.Bot
	baseGroup  *telegohandler.HandlerGroup
	store      *routeTestStore
	caller     *routeTestCaller
	events     *routeEventSink
}

func newRouteTestHarness(t *testing.T) routeTestHarness {
	t.Helper()
	return newRouteTestHarnessWithCleaner(t, nil)
}

type routeTestCleaner struct {
	deleteEventSubsFn func(ctx context.Context, creatorID string) error
}

func (c routeTestCleaner) DeleteEventSubsForCreator(ctx context.Context, creatorID string) error {
	if c.deleteEventSubsFn != nil {
		return c.deleteEventSubsFn(ctx, creatorID)
	}
	return nil
}

func newRouteTestHarnessWithCleaner(t *testing.T, cleaner usecaseGroupUnregistrationCleaner) routeTestHarness {
	t.Helper()

	caller := &routeTestCaller{}
	bot, err := telego.NewBot("123456:"+strings.Repeat("a", 35), telego.WithAPICaller(caller))
	if err != nil {
		t.Fatalf("telego.NewBot() error = %v", err)
	}

	bh, err := telegohandler.NewBotHandler(bot, nil)
	if err != nil {
		t.Fatalf("telegohandler.NewBotHandler() error = %v", err)
	}

	store := &routeTestStore{}
	limiter := ratelimit.NewRateLimiter(1000, 0)
	t.Cleanup(limiter.Close)
	tgClient := telegramclient.New(bot, limiter, nil)
	tgGroups := telegramgroups.New(bot, limiter, nil, store, nil)
	eventSink := &routeEventSink{}
	tgClient.SetObserver(eventSink)

	controller := New(Dependencies{
		Config: config.Config{
			PublicBaseURL: "https://example.com",
		},
		Store:               store,
		TelegramLimiter:     limiter,
		TelegramBot:         bot,
		TelegramHandler:     bh,
		TelegramClient:      tgClient,
		TelegramGroups:      tgGroups,
		CreatorStatus:       usecase.NewCreatorStatusUseCase(core.NewCreatorService(store, routeTestEventSubChecker{}, nil), eventSink),
		GroupRegistration:   usecase.NewGroupRegistrationUseCase(store, eventSink),
		GroupUnregistration: usecase.NewGroupUnregistrationUseCase(store, cleaner, nil, eventSink),
		GroupPolicyUpdate:   usecase.NewGroupPolicyUpdateUseCase(store, eventSink),
		GroupLanguageUpdate: usecase.NewGroupLanguageUpdateUseCase(store, eventSink),
		Privacy:             usecase.NewPrivacyUseCase(store, "v1", 24*time.Hour),
		Events:              eventSink,
	})
	controller.SetViewerAccessUseCase(usecase.NewViewerAccessUseCase(core.NewViewerService(store, controller.ViewerGroupOps(), nil, nil, nil), nil, eventSink))
	controller.SetResetUseCase(usecase.NewResetUseCase(core.NewResetService(store, controller.KickFromGroup, nil, nil), eventSink))
	controller.RegisterTelegramHandlers()

	return routeTestHarness{
		controller: controller,
		bot:        bot,
		baseGroup:  bh.BaseGroup(),
		store:      store,
		caller:     caller,
		events:     eventSink,
	}
}

type routeEventSink struct {
	mu     sync.Mutex
	events []events.Event
}

func (s *routeEventSink) Emit(_ context.Context, evt events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
}

func (s *routeEventSink) snapshot() []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]events.Event(nil), s.events...)
}

type usecaseGroupUnregistrationCleaner interface {
	DeleteEventSubsForCreator(ctx context.Context, creatorID string) error
}

func (h routeTestHarness) handleUpdate(t *testing.T, update telego.Update) {
	t.Helper()

	if err := h.baseGroup.HandleUpdate(t.Context(), h.bot, update); err != nil {
		t.Fatalf("HandleUpdate(%+v) error = %v, want nil", update, err)
	}
}

func (h routeTestHarness) assertOAuthPromptSaved(t *testing.T, wantCalls int, wantMode core.OAuthMode, wantUserID int64, wantPromptMessageID int) {
	t.Helper()

	if got := h.store.saveOAuthStateCallCount(); got != wantCalls {
		t.Fatalf("SaveOAuthState call count = %d, want %d", got, wantCalls)
	}
	last := h.store.lastSavedStatePayload()
	if last.Mode != wantMode {
		t.Fatalf("last saved payload mode = %q, want %q", last.Mode, wantMode)
	}
	if last.TelegramUserID != wantUserID {
		t.Fatalf("last saved payload telegram user = %d, want %d", last.TelegramUserID, wantUserID)
	}
	if last.PromptMessageID != wantPromptMessageID {
		t.Fatalf("last saved payload prompt message id = %d, want %d", last.PromptMessageID, wantPromptMessageID)
	}
	if ttl := h.store.lastSavedStateTTL(); ttl != core.OAuthStateTTL {
		t.Fatalf("last saved payload ttl = %s, want %s", ttl, core.OAuthStateTTL)
	}
}

func (h routeTestHarness) assertEditMessageHasCallback(t *testing.T, body json.RawMessage, want string) {
	t.Helper()

	got := parseEditMessageRequest(t, body)
	for _, row := range got.ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == want {
				return
			}
		}
	}
	t.Fatalf("editMessageText callback data = %+v, want %q", got.ReplyMarkup.InlineKeyboard, want)
}

func (h routeTestHarness) assertEditMessageLacksCallback(t *testing.T, body json.RawMessage, unwanted string) {
	t.Helper()

	got := parseEditMessageRequest(t, body)
	for _, row := range got.ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == unwanted {
				t.Fatalf("editMessageText callback data = %+v, did not expect %q", got.ReplyMarkup.InlineKeyboard, unwanted)
			}
		}
	}
}

func (h routeTestHarness) assertEditMessageTextContains(t *testing.T, body json.RawMessage, want string) {
	t.Helper()

	got := parseEditMessageRequest(t, body)
	if !strings.Contains(got.Text, want) {
		t.Fatalf("editMessageText text = %q, want substring %q", got.Text, want)
	}
}

func (h routeTestHarness) assertSendMessageHasCallback(t *testing.T, body json.RawMessage, want string) {
	t.Helper()

	got := parseSendMessageRequest(t, body)
	for _, row := range got.ReplyMarkup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == want {
				return
			}
		}
	}
	t.Fatalf("sendMessage callback data = %+v, want %q", got.ReplyMarkup.InlineKeyboard, want)
}

func (h routeTestHarness) assertEmittedEvent(t *testing.T, wantName, wantOutcome string, wantFields map[string]string) {
	t.Helper()

	for _, evt := range h.events.snapshot() {
		if evt.Name != wantName {
			continue
		}
		if wantOutcome != "" && evt.Outcome != wantOutcome {
			continue
		}
		match := true
		for k, v := range wantFields {
			if evt.Fields[k] != v {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Fatalf("did not find event name=%q outcome=%q fields=%v in %+v", wantName, wantOutcome, wantFields, h.events.snapshot())
}

func parseEditMessageRequest(t *testing.T, body json.RawMessage) editMessageRequest {
	t.Helper()

	var got editMessageRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json.Unmarshal(editMessageText body) error = %v, body = %s", err, body)
	}
	return got
}

func parseSendMessageRequest(t *testing.T, body json.RawMessage) sendMessageRequest {
	t.Helper()

	var got sendMessageRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json.Unmarshal(sendMessage body) error = %v, body = %s", err, body)
	}
	return got
}

type routeTestCaller struct {
	mu                  sync.Mutex
	methods             []string
	requestBodies       map[string][]json.RawMessage
	errByMethod         map[string]error
	botUserID           int64
	chatMembersByUserID map[int64]json.RawMessage
	getChatResult       json.RawMessage
	getChatMemberCount  int
	getChatAdminsResult json.RawMessage
}

func mustMarshalJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func (c *routeTestCaller) setBotUserID() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.botUserID = 999
}

func (c *routeTestCaller) setChatMember(userID int64, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chatMembersByUserID == nil {
		c.chatMembersByUserID = make(map[int64]json.RawMessage)
	}
	c.chatMembersByUserID[userID] = raw
}

func (c *routeTestCaller) setMethodError(method string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errByMethod == nil {
		c.errByMethod = make(map[string]error)
	}
	c.errByMethod[method] = err
}

func (c *routeTestCaller) setChatMemberCount(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getChatMemberCount = count
}

func (c *routeTestCaller) setChatAdministrators(raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getChatAdminsResult = raw
}

func (c *routeTestCaller) Call(_ context.Context, url string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	method := url[strings.LastIndex(url, "/")+1:]

	c.mu.Lock()
	c.methods = append(c.methods, method)
	if c.requestBodies == nil {
		c.requestBodies = make(map[string][]json.RawMessage)
	}
	c.requestBodies[method] = append(c.requestBodies[method], append(json.RawMessage(nil), data.BodyRaw...))
	botUserID := c.botUserID
	chatMembersByUserID := c.chatMembersByUserID
	getChatResult := c.getChatResult
	getChatMemberCount := c.getChatMemberCount
	getChatAdminsResult := c.getChatAdminsResult
	methodErr := c.errByMethod[method]
	c.mu.Unlock()

	if methodErr != nil {
		return nil, methodErr
	}

	switch method {
	case "sendMessage", "editMessageText":
		return &telegoapi.Response{
			Ok: true,
			Result: json.RawMessage(`{
				"message_id": 101,
				"date": 0,
				"chat": {"id": 1, "type": "private"}
			}`),
		}, nil
	case "answerCallbackQuery", "approveChatJoinRequest", "declineChatJoinRequest", "banChatMember", "unbanChatMember", "setChatMemberTag":
		return &telegoapi.Response{
			Ok:     true,
			Result: json.RawMessage(`true`),
		}, nil
	case "getMe":
		if botUserID == 0 {
			botUserID = 999
		}
		return &telegoapi.Response{
			Ok: true,
			Result: mustMarshalJSON(telego.User{
				ID:        botUserID,
				IsBot:     true,
				FirstName: "ImSub",
				Username:  "imsub_bot",
			}),
		}, nil
	case "getChatMember":
		var params struct {
			UserID int64 `json:"user_id"`
		}
		if err := json.Unmarshal(data.BodyRaw, &params); err != nil {
			return nil, fmt.Errorf("decode getChatMember request: %w", err)
		}
		if raw, ok := chatMembersByUserID[params.UserID]; ok {
			return &telegoapi.Response{Ok: true, Result: raw}, nil
		}
		return &telegoapi.Response{
			Ok: true,
			Result: json.RawMessage(`{
				"status": "member",
				"user": {"id": 1, "is_bot": false, "first_name": "Member"}
			}`),
		}, nil
	case "getChat":
		if len(getChatResult) == 0 {
			getChatResult = json.RawMessage(`{
				"id": -100,
				"type": "supergroup",
				"title": "VIP",
				"join_by_request": true
			}`)
		}
		return &telegoapi.Response{Ok: true, Result: getChatResult}, nil
	case "getChatMemberCount":
		count := getChatMemberCount
		if count == 0 {
			count = 1
		}
		return &telegoapi.Response{Ok: true, Result: json.RawMessage(strconv.Itoa(count))}, nil
	case "getChatAdministrators":
		if len(getChatAdminsResult) == 0 {
			getChatAdminsResult = json.RawMessage(`[]`)
		}
		return &telegoapi.Response{Ok: true, Result: getChatAdminsResult}, nil
	default:
		return nil, fmt.Errorf("%w %q", errUnexpectedTelegramMethod, method)
	}
}

func (c *routeTestCaller) assertExactMethods(t *testing.T, want ...string) {
	t.Helper()

	c.mu.Lock()
	got := append([]string(nil), c.methods...)
	c.mu.Unlock()

	if len(got) != len(want) {
		t.Fatalf("Telegram methods = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Telegram methods = %#v, want %#v", got, want)
		}
	}
}

func (c *routeTestCaller) lastEditMessageBody() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	bodies := c.requestBodies["editMessageText"]
	if len(bodies) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), bodies[len(bodies)-1]...)
}

func (c *routeTestCaller) lastSendMessageBody() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	bodies := c.requestBodies["sendMessage"]
	if len(bodies) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), bodies[len(bodies)-1]...)
}

func (c *routeTestCaller) lastAnswerCallbackBody() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	bodies := c.requestBodies["answerCallbackQuery"]
	if len(bodies) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), bodies[len(bodies)-1]...)
}

func (c *routeTestCaller) lastSetChatMemberTagBody() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	bodies := c.requestBodies["setChatMemberTag"]
	if len(bodies) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), bodies[len(bodies)-1]...)
}

type routeTestStore struct {
	routeTestStoreStub

	mu                      sync.Mutex
	saveOAuthStateCalls     int
	savedOAuthStateCalls    []core.OAuthStatePayload
	savedOAuthStateTTLs     []time.Duration
	deleteOAuthStateCalls   int
	viewerIdentity          core.UserIdentity
	viewerIdentityOK        bool
	creatorByID             map[string]core.Creator
	ownedCreator            core.Creator
	ownedCreatorOK          bool
	managedGroupsByChatID   map[int64]core.ManagedGroup
	trackedMembersByGroup   map[int64]map[int64]bool
	untrackedUpserts        []routeTestUntrackedUpsert
	untrackedMembersByGroup map[int64]map[int64]core.UntrackedGroupMember
	untrackedCountByChatID  map[int64]int
	managedTagsByGroup      map[int64]map[int64]core.ManagedMemberTag
	subscribersByCreator    map[string]map[string]bool
	blockedByCreatorUser    map[string]map[string]bool
	activeUsers             []int64
	consentRecord           core.ConsentRecord
	consentOK               bool
	privacyReceipts         []core.PrivacyReceipt
}

type routeTestUntrackedUpsert struct {
	chatID         int64
	telegramUserID int64
	source         string
	status         string
}

func (s *routeTestStore) setOwnedCreator(creator core.Creator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ownedCreator = creator
	s.ownedCreatorOK = true
	if s.creatorByID == nil {
		s.creatorByID = make(map[string]core.Creator)
	}
	s.creatorByID[creator.ID] = creator
}

func (s *routeTestStore) setViewerIdentity(identity core.UserIdentity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.viewerIdentity = identity
	s.viewerIdentityOK = true
}

func (s *routeTestStore) setManagedGroup(group core.ManagedGroup) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedGroupsByChatID == nil {
		s.managedGroupsByChatID = make(map[int64]core.ManagedGroup)
	}
	s.managedGroupsByChatID[group.ChatID] = group
}

func (s *routeTestStore) setCreatorBlocked(creatorID, twitchUserID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockedByCreatorUser == nil {
		s.blockedByCreatorUser = make(map[string]map[string]bool)
	}
	if s.blockedByCreatorUser[creatorID] == nil {
		s.blockedByCreatorUser[creatorID] = make(map[string]bool)
	}
	s.blockedByCreatorUser[creatorID][twitchUserID] = true
}

func (s *routeTestStore) hasManagedGroup(chatID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.managedGroupsByChatID[chatID]
	return ok
}

func (s *routeTestStore) lastUntrackedMemberUpsert() routeTestUntrackedUpsert {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.untrackedUpserts) == 0 {
		return routeTestUntrackedUpsert{}
	}
	return s.untrackedUpserts[len(s.untrackedUpserts)-1]
}

func (s *routeTestStore) saveOAuthStateCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveOAuthStateCalls
}

func (s *routeTestStore) deleteOAuthStateCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteOAuthStateCalls
}

func (s *routeTestStore) lastSavedStatePayload() core.OAuthStatePayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.savedOAuthStateCalls) == 0 {
		return core.OAuthStatePayload{}
	}
	return s.savedOAuthStateCalls[len(s.savedOAuthStateCalls)-1]
}

func (s *routeTestStore) lastSavedStateTTL() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.savedOAuthStateTTLs) == 0 {
		return 0
	}
	return s.savedOAuthStateTTLs[len(s.savedOAuthStateTTLs)-1]
}

func (s *routeTestStore) activeUserTouches() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.activeUsers...)
}

func (s *routeTestStore) TrackTelegramActiveUser(_ context.Context, telegramUserID int64, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeUsers = append(s.activeUsers, telegramUserID)
	return nil
}

func (s *routeTestStore) SaveOAuthState(_ context.Context, _ string, payload core.OAuthStatePayload, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveOAuthStateCalls++
	s.savedOAuthStateCalls = append(s.savedOAuthStateCalls, payload)
	s.savedOAuthStateTTLs = append(s.savedOAuthStateTTLs, ttl)
	return nil
}

func (s *routeTestStore) ConsentRecord(_ context.Context, telegramUserID int64) (core.ConsentRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.consentOK || s.consentRecord.TelegramUserID != telegramUserID {
		return core.ConsentRecord{}, false, nil
	}
	return s.consentRecord, true, nil
}

func (s *routeTestStore) SaveConsentRecord(_ context.Context, record core.ConsentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consentRecord = record
	s.consentOK = true
	return nil
}

func (s *routeTestStore) DeleteConsentRecord(_ context.Context, telegramUserID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consentOK && s.consentRecord.TelegramUserID == telegramUserID {
		s.consentOK = false
		s.consentRecord = core.ConsentRecord{}
	}
	return nil
}

func (s *routeTestStore) DeleteOAuthState(_ context.Context, _ string) (core.OAuthStatePayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteOAuthStateCalls++
	return core.OAuthStatePayload{}, nil
}

func (s *routeTestStore) ListUntrackedMembershipsForUser(_ context.Context, telegramUserID int64) ([]core.UntrackedGroupMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.UntrackedGroupMember
	for _, members := range s.untrackedMembersByGroup {
		if member, ok := members[telegramUserID]; ok {
			out = append(out, member)
		}
	}
	return out, nil
}

func (s *routeTestStore) DeleteAllUntrackedMembershipsForUser(_ context.Context, telegramUserID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for chatID, members := range s.untrackedMembersByGroup {
		if _, ok := members[telegramUserID]; ok {
			delete(members, telegramUserID)
			count++
			s.untrackedMembersByGroup[chatID] = members
		}
	}
	return count, nil
}

func (s *routeTestStore) ListTrackedGroupIDsForUser(_ context.Context, telegramUserID int64) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int64
	for chatID, members := range s.trackedMembersByGroup {
		if members[telegramUserID] {
			out = append(out, chatID)
		}
	}
	return out, nil
}

func (s *routeTestStore) DeleteOAuthStatesForUser(context.Context, int64) (int, error) {
	return 0, nil
}

func (s *routeTestStore) ListPrivacyReceipts(_ context.Context, telegramUserID int64) ([]core.PrivacyReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.PrivacyReceipt, 0, len(s.privacyReceipts))
	for _, receipt := range s.privacyReceipts {
		if receipt.TelegramUserID == telegramUserID {
			out = append(out, receipt)
		}
	}
	return out, nil
}

func (s *routeTestStore) SavePrivacyReceipt(_ context.Context, receipt core.PrivacyReceipt, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.privacyReceipts = append(s.privacyReceipts, receipt)
	return nil
}

func (s *routeTestStore) DeletePrivacyArtifacts(_ context.Context, telegramUserID int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consentOK && s.consentRecord.TelegramUserID == telegramUserID {
		s.consentOK = false
		s.consentRecord = core.ConsentRecord{}
	}
	s.privacyReceipts = nil
	return nil
}

func (s *routeTestStore) UserIdentity(_ context.Context, telegramUserID int64) (core.UserIdentity, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.viewerIdentityOK || s.viewerIdentity.TelegramUserID != telegramUserID {
		return core.UserIdentity{}, false, nil
	}
	return s.viewerIdentity, true, nil
}

func (s *routeTestStore) OwnedCreatorForUser(_ context.Context, ownerTelegramID int64) (core.Creator, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ownedCreatorOK || s.ownedCreator.OwnerTelegramID != ownerTelegramID {
		return core.Creator{}, false, nil
	}
	return s.ownedCreator, true, nil
}

func (s *routeTestStore) Creator(_ context.Context, creatorID string) (core.Creator, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creatorByID != nil {
		creator, ok := s.creatorByID[creatorID]
		return creator, ok, nil
	}
	if s.ownedCreatorOK && s.ownedCreator.ID == creatorID {
		return s.ownedCreator, true, nil
	}
	return core.Creator{}, false, nil
}

func (s *routeTestStore) UpdateCreatorSubscriptionEndGrace(_ context.Context, creatorID string, grace core.SubscriptionEndGrace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creatorByID == nil {
		s.creatorByID = make(map[string]core.Creator)
	}
	creator, ok := s.creatorByID[creatorID]
	if !ok && s.ownedCreatorOK && s.ownedCreator.ID == creatorID {
		creator = s.ownedCreator
		ok = true
	}
	if !ok {
		return nil
	}
	creator.SubscriptionEndGrace = grace
	s.creatorByID[creatorID] = creator
	if s.ownedCreatorOK && s.ownedCreator.ID == creatorID {
		s.ownedCreator = creator
	}
	return nil
}

func (s *routeTestStore) ResolveTelegramUserIDByTwitch(_ context.Context, twitchUserID string) (int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.viewerIdentityOK || s.viewerIdentity.TwitchUserID != twitchUserID {
		return 0, false, nil
	}
	return s.viewerIdentity.TelegramUserID, true, nil
}

func (s *routeTestStore) DeleteSubscriptionEndGrace(context.Context, string, string) error {
	return nil
}

func (s *routeTestStore) ManagedGroupByChatID(_ context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, ok := s.managedGroupsByChatID[chatID]
	return group, ok, nil
}

func (s *routeTestStore) IsCreatorBlocked(_ context.Context, creatorID, twitchUserID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockedByCreatorUser == nil || s.blockedByCreatorUser[creatorID] == nil {
		return false, nil
	}
	return s.blockedByCreatorUser[creatorID][twitchUserID], nil
}

func (s *routeTestStore) IsCreatorSubscriber(_ context.Context, creatorID, twitchUserID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subscribersByCreator == nil || s.subscribersByCreator[creatorID] == nil {
		return false, nil
	}
	return s.subscribersByCreator[creatorID][twitchUserID], nil
}

func (s *routeTestStore) ListManagedGroupsByCreator(_ context.Context, creatorID string) ([]core.ManagedGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	groups := make([]core.ManagedGroup, 0, len(s.managedGroupsByChatID))
	for _, group := range s.managedGroupsByChatID {
		if group.CreatorID == creatorID {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func (s *routeTestStore) ListTrackedGroupMemberIDs(_ context.Context, chatID int64) ([]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trackedMembersByGroup == nil || s.trackedMembersByGroup[chatID] == nil {
		return nil, nil
	}
	out := make([]int64, 0, len(s.trackedMembersByGroup[chatID]))
	for telegramUserID := range s.trackedMembersByGroup[chatID] {
		out = append(out, telegramUserID)
	}
	return out, nil
}

func (s *routeTestStore) CreateMemberCleanupJob(_ context.Context, job core.MemberCleanupJob) (core.MemberCleanupJob, error) {
	job.ID = "job-1"
	job.TotalTargets = len(job.Targets)
	return job, nil
}

func (s *routeTestStore) CreatorBlockedUserCount(_ context.Context, creatorID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blockedByCreatorUser == nil || s.blockedByCreatorUser[creatorID] == nil {
		return 0, nil
	}
	return int64(len(s.blockedByCreatorUser[creatorID])), nil
}

func (s *routeTestStore) DeleteManagedGroup(_ context.Context, chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.managedGroupsByChatID, chatID)
	return nil
}

func (s *routeTestStore) UpsertManagedGroup(_ context.Context, group core.ManagedGroup) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedGroupsByChatID == nil {
		s.managedGroupsByChatID = make(map[int64]core.ManagedGroup)
	}
	s.managedGroupsByChatID[group.ChatID] = group
	return nil
}

func (s *routeTestStore) IsTrackedGroupMember(_ context.Context, chatID, telegramUserID int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trackedMembersByGroup == nil || s.trackedMembersByGroup[chatID] == nil {
		return false, nil
	}
	return s.trackedMembersByGroup[chatID][telegramUserID], nil
}

func (s *routeTestStore) AddTrackedGroupMember(_ context.Context, chatID, telegramUserID int64, _ string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trackedMembersByGroup == nil {
		s.trackedMembersByGroup = make(map[int64]map[int64]bool)
	}
	if s.trackedMembersByGroup[chatID] == nil {
		s.trackedMembersByGroup[chatID] = make(map[int64]bool)
	}
	s.trackedMembersByGroup[chatID][telegramUserID] = true
	return nil
}

func (s *routeTestStore) RemoveTrackedGroupMember(_ context.Context, chatID, telegramUserID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.trackedMembersByGroup[chatID] != nil {
		delete(s.trackedMembersByGroup[chatID], telegramUserID)
	}
	return nil
}

func (s *routeTestStore) UpsertUntrackedGroupMember(_ context.Context, chatID, telegramUserID int64, source, status string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.untrackedMembersByGroup == nil {
		s.untrackedMembersByGroup = make(map[int64]map[int64]core.UntrackedGroupMember)
	}
	if s.untrackedMembersByGroup[chatID] == nil {
		s.untrackedMembersByGroup[chatID] = make(map[int64]core.UntrackedGroupMember)
	}
	s.untrackedMembersByGroup[chatID][telegramUserID] = core.UntrackedGroupMember{
		ChatID:         chatID,
		TelegramUserID: telegramUserID,
		Source:         source,
		LastStatus:     status,
	}
	if s.untrackedCountByChatID == nil {
		s.untrackedCountByChatID = make(map[int64]int)
	}
	s.untrackedCountByChatID[chatID] = len(s.untrackedMembersByGroup[chatID])
	s.untrackedUpserts = append(s.untrackedUpserts, routeTestUntrackedUpsert{
		chatID:         chatID,
		telegramUserID: telegramUserID,
		source:         source,
		status:         status,
	})
	return nil
}

func (s *routeTestStore) RemoveUntrackedGroupMember(_ context.Context, chatID, telegramUserID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.untrackedMembersByGroup != nil && s.untrackedMembersByGroup[chatID] != nil {
		delete(s.untrackedMembersByGroup[chatID], telegramUserID)
	}
	if s.untrackedCountByChatID != nil {
		s.untrackedCountByChatID[chatID] = len(s.untrackedMembersByGroup[chatID])
	}
	return nil
}

func (s *routeTestStore) CountUntrackedGroupMembers(_ context.Context, chatID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.untrackedCountByChatID == nil {
		return 0, nil
	}
	return s.untrackedCountByChatID[chatID], nil
}

func (s *routeTestStore) ListUntrackedGroupMembers(_ context.Context, chatID int64) ([]core.UntrackedGroupMember, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.untrackedMembersByGroup == nil || s.untrackedMembersByGroup[chatID] == nil {
		return nil, nil
	}
	out := make([]core.UntrackedGroupMember, 0, len(s.untrackedMembersByGroup[chatID]))
	for _, member := range s.untrackedMembersByGroup[chatID] {
		out = append(out, member)
	}
	return out, nil
}

func (s *routeTestStore) UpsertManagedMemberTag(_ context.Context, item core.ManagedMemberTag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedTagsByGroup == nil {
		s.managedTagsByGroup = make(map[int64]map[int64]core.ManagedMemberTag)
	}
	if s.managedTagsByGroup[item.ChatID] == nil {
		s.managedTagsByGroup[item.ChatID] = make(map[int64]core.ManagedMemberTag)
	}
	s.managedTagsByGroup[item.ChatID][item.TelegramUserID] = item
	return nil
}

func (s *routeTestStore) RemoveManagedMemberTag(_ context.Context, chatID, telegramUserID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedTagsByGroup != nil && s.managedTagsByGroup[chatID] != nil {
		delete(s.managedTagsByGroup[chatID], telegramUserID)
	}
	return nil
}

func (s *routeTestStore) RemoveManagedMemberTagsForGroup(_ context.Context, chatID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedTagsByGroup != nil {
		delete(s.managedTagsByGroup, chatID)
	}
	return nil
}

func (s *routeTestStore) UpdateManagedGroupMemberTagSyncEnabled(_ context.Context, chatID int64, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedGroupsByChatID == nil {
		return nil
	}
	group, ok := s.managedGroupsByChatID[chatID]
	if !ok {
		return nil
	}
	group.MemberTagSyncEnabled = enabled
	s.managedGroupsByChatID[chatID] = group
	return nil
}

func (s *routeTestStore) ListManagedMemberTags(_ context.Context, chatID int64) ([]core.ManagedMemberTag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.managedTagsByGroup == nil || s.managedTagsByGroup[chatID] == nil {
		return nil, nil
	}
	out := make([]core.ManagedMemberTag, 0, len(s.managedTagsByGroup[chatID]))
	for _, item := range s.managedTagsByGroup[chatID] {
		out = append(out, item)
	}
	return out, nil
}

func routeTestAdminMemberJSON(userID int64, isBot bool, canInviteUsers bool, canRestrictMembers bool) json.RawMessage {
	return routeTestAdminMemberJSONWithTags(userID, isBot, canInviteUsers, canRestrictMembers, false)
}

func routeTestAdminMemberJSONWithTags(userID int64, isBot bool, canInviteUsers bool, canRestrictMembers bool, canManageTags bool) json.RawMessage {
	return mustMarshalJSON(map[string]any{
		"status": "administrator",
		"user": map[string]any{
			"id":         userID,
			"is_bot":     isBot,
			"first_name": "Member",
		},
		"can_be_edited":          false,
		"is_anonymous":           false,
		"can_manage_chat":        true,
		"can_delete_messages":    true,
		"can_manage_video_chats": false,
		"can_restrict_members":   canRestrictMembers,
		"can_promote_members":    false,
		"can_change_info":        false,
		"can_invite_users":       canInviteUsers,
		"can_post_stories":       false,
		"can_edit_stories":       false,
		"can_delete_stories":     false,
		"can_manage_tags":        canManageTags,
	})
}

type routeTestStoreStub struct{}

func (routeTestStoreStub) Ping(context.Context) error { return nil }
func (routeTestStoreStub) Close() error               { return nil }
func (routeTestStoreStub) EnsureSchema(context.Context) error {
	return nil
}
func (routeTestStoreStub) UserIdentity(context.Context, int64) (core.UserIdentity, bool, error) {
	return core.UserIdentity{}, false, nil
}
func (routeTestStoreStub) SaveUserIdentityOnly(context.Context, int64, string, string, string) (int64, error) {
	return 0, nil
}
func (routeTestStoreStub) RemoveUserCreatorByTwitch(context.Context, string, string) (int64, bool, error) {
	return 0, false, nil
}
func (routeTestStoreStub) DeleteAllUserData(context.Context, int64) error { return nil }
func (routeTestStoreStub) ManagedGroupByChatID(context.Context, int64) (core.ManagedGroup, bool, error) {
	return core.ManagedGroup{}, false, nil
}
func (routeTestStoreStub) ListManagedGroups(context.Context) ([]core.ManagedGroup, error) {
	return nil, nil
}
func (routeTestStoreStub) ListManagedGroupsByCreator(context.Context, string) ([]core.ManagedGroup, error) {
	return nil, nil
}
func (routeTestStoreStub) ListTrackedGroupIDsForUser(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (routeTestStoreStub) ListTrackedGroupMemberIDs(context.Context, int64) ([]int64, error) {
	return nil, nil
}
func (routeTestStoreStub) CreateMemberCleanupJob(context.Context, core.MemberCleanupJob) (core.MemberCleanupJob, error) {
	return core.MemberCleanupJob{}, nil
}
func (routeTestStoreStub) UpsertManagedGroup(context.Context, core.ManagedGroup) error { return nil }
func (routeTestStoreStub) DeleteManagedGroup(context.Context, int64) error             { return nil }
func (routeTestStoreStub) AddTrackedGroupMember(context.Context, int64, int64, string, time.Time) error {
	return nil
}
func (routeTestStoreStub) RemoveTrackedGroupMember(context.Context, int64, int64) error { return nil }
func (routeTestStoreStub) IsTrackedGroupMember(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (routeTestStoreStub) TrackTelegramActiveUser(context.Context, int64, time.Time) error {
	return nil
}
func (routeTestStoreStub) UpsertUntrackedGroupMember(context.Context, int64, int64, string, string, time.Time) error {
	return nil
}
func (routeTestStoreStub) RemoveUntrackedGroupMember(context.Context, int64, int64) error { return nil }
func (routeTestStoreStub) CountUntrackedGroupMembers(context.Context, int64) (int, error) {
	return 0, nil
}
func (routeTestStoreStub) ListUntrackedGroupMembers(context.Context, int64) ([]core.UntrackedGroupMember, error) {
	return nil, nil
}
func (routeTestStoreStub) UpsertManagedMemberTag(context.Context, core.ManagedMemberTag) error {
	return nil
}
func (routeTestStoreStub) RemoveManagedMemberTag(context.Context, int64, int64) error   { return nil }
func (routeTestStoreStub) RemoveManagedMemberTagsForGroup(context.Context, int64) error { return nil }
func (routeTestStoreStub) UpdateManagedGroupMemberTagSyncEnabled(context.Context, int64, bool) error {
	return nil
}
func (routeTestStoreStub) ListManagedMemberTags(context.Context, int64) ([]core.ManagedMemberTag, error) {
	return nil, nil
}
func (routeTestStoreStub) Creator(context.Context, string) (core.Creator, bool, error) {
	return core.Creator{}, false, nil
}
func (routeTestStoreStub) ListCreators(context.Context) ([]core.Creator, error) { return nil, nil }
func (routeTestStoreStub) ListActiveCreators(context.Context) ([]core.Creator, error) {
	return nil, nil
}
func (routeTestStoreStub) ListActiveCreatorGroups(context.Context) ([]core.ActiveCreatorGroups, error) {
	return nil, nil
}
func (routeTestStoreStub) LoadCreatorsByIDs(context.Context, []string, func(core.Creator) bool) ([]core.Creator, error) {
	return nil, nil
}
func (routeTestStoreStub) UpsertCreator(context.Context, core.Creator) error { return nil }
func (routeTestStoreStub) ResolveTelegramUserIDByTwitch(context.Context, string) (int64, bool, error) {
	return 0, false, nil
}
func (routeTestStoreStub) UpdateCreatorSubscriptionEndGrace(context.Context, string, core.SubscriptionEndGrace) error {
	return nil
}
func (routeTestStoreStub) DeleteSubscriptionEndGrace(context.Context, string, string) error {
	return nil
}
func (routeTestStoreStub) DeleteCreatorData(context.Context, int64) (int, []string, error) {
	return 0, nil, nil
}
func (routeTestStoreStub) UpdateCreatorTokens(context.Context, string, string, string, []string) error {
	return nil
}
func (routeTestStoreStub) IsCreatorBlocked(context.Context, string, string) (bool, error) {
	return false, nil
}
func (routeTestStoreStub) MarkCreatorAuthReconnectRequired(context.Context, string, string, time.Time) (bool, error) {
	return false, nil
}
func (routeTestStoreStub) MarkCreatorAuthHealthy(context.Context, string, time.Time) error {
	return nil
}
func (routeTestStoreStub) UpdateCreatorLastSync(context.Context, string, time.Time) error { return nil }
func (routeTestStoreStub) UpdateCreatorLastReconnectNotice(context.Context, string, time.Time) error {
	return nil
}
func (routeTestStoreStub) CreatorAuthReconnectRequiredCount(context.Context) (int, error) {
	return 0, nil
}
func (routeTestStoreStub) OAuthState(context.Context, string) (core.OAuthStatePayload, error) {
	return core.OAuthStatePayload{}, nil
}
func (routeTestStoreStub) DeleteOAuthState(context.Context, string) (core.OAuthStatePayload, error) {
	return core.OAuthStatePayload{}, nil
}
func (routeTestStoreStub) IsCreatorSubscriber(context.Context, string, string) (bool, error) {
	return false, nil
}
func (routeTestStoreStub) AddCreatorSubscriber(context.Context, string, string) error    { return nil }
func (routeTestStoreStub) RemoveCreatorSubscriber(context.Context, string, string) error { return nil }
func (routeTestStoreStub) CreatorSubscriberCount(context.Context, string) (int64, error) {
	return 0, nil
}
func (routeTestStoreStub) NewSubscriberDumpKey(string) string { return "" }
func (routeTestStoreStub) AddToSubscriberDump(context.Context, string, []string) error {
	return nil
}
func (routeTestStoreStub) FinalizeSubscriberDump(context.Context, string, string, bool) error {
	return nil
}
func (routeTestStoreStub) CleanupSubscriberDump(context.Context, string) {}
func (routeTestStoreStub) MarkEventProcessed(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func (routeTestStoreStub) RepairTrackedGroupReverseIndex(context.Context) (
	indexUsers int,
	repairedUsers int,
	missingLinks int,
	staleLinks int,
	err error,
) {
	return 0, 0, 0, 0, nil
}
func (routeTestStoreStub) ActiveCreatorIDsWithoutGroup(context.Context, []core.Creator) (int, error) {
	return 0, nil
}

type routeTestEventSubChecker struct{}

func (routeTestEventSubChecker) IsEventSubActiveForCreator(context.Context, string) (bool, error) {
	return false, nil
}
