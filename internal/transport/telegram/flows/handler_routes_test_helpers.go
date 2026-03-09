package flows

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

	"imsub/internal/application"
	"imsub/internal/core"
	"imsub/internal/platform/config"
	"imsub/internal/platform/i18n"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
)

var errUnexpectedTelegramMethod = errors.New("unexpected Telegram method")

type editMessageRequest struct {
	ReplyMarkup struct {
		InlineKeyboard [][]struct {
			CallbackData string `json:"callback_data"`
			URL          string `json:"url"`
		} `json:"inline_keyboard"`
	} `json:"reply_markup"`
}

type routeTestHarness struct {
	bot       *telego.Bot
	baseGroup *telegohandler.HandlerGroup
	store     *routeTestStore
	caller    *routeTestCaller
}

func newRouteTestHarness(t *testing.T) routeTestHarness {
	t.Helper()

	if err := i18n.Ensure(); err != nil {
		t.Fatalf("i18n.Ensure() error = %v", err)
	}

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
	appRuntime := application.NewRuntime(application.Dependencies{
		CreatorStatus:       usecase.NewCreatorStatusUseCase(core.NewCreator(store, routeTestEventSubChecker{}, nil), nil),
		GroupRegistration:   usecase.NewGroupRegistrationUseCase(store, nil),
		GroupUnregistration: usecase.NewGroupUnregistrationUseCase(store, nil, nil),
		NewViewerAccess: func(groupOps core.GroupOps) *usecase.ViewerAccessUseCase {
			return usecase.NewViewerAccessUseCase(core.NewViewer(store, groupOps, nil, nil), nil)
		},
		NewReset: func(kick func(ctx context.Context, groupChatID, telegramUserID int64) error) *usecase.ResetUseCase {
			return usecase.NewResetUseCase(core.NewResetter(store, kick, nil), nil)
		},
	})

	controller := New(Dependencies{
		Config: config.Config{
			PublicBaseURL: "https://example.com",
		},
		Store:           store,
		TelegramLimiter: limiter,
		TelegramBot:     bot,
		TelegramHandler: bh,
		App:             appRuntime,
	})
	controller.RegisterTelegramHandlers()

	return routeTestHarness{
		bot:       bot,
		baseGroup: bh.BaseGroup(),
		store:     store,
		caller:    caller,
	}
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

func parseEditMessageRequest(t *testing.T, body json.RawMessage) editMessageRequest {
	t.Helper()

	var got editMessageRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("json.Unmarshal(editMessageText body) error = %v, body = %s", err, body)
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

func (c *routeTestCaller) setBotUserID(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.botUserID = id
}

func (c *routeTestCaller) setChatMember(userID int64, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.chatMembersByUserID == nil {
		c.chatMembersByUserID = make(map[int64]json.RawMessage)
	}
	c.chatMembersByUserID[userID] = raw
}

func (c *routeTestCaller) setChatResult(raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getChatResult = raw
}

func (c *routeTestCaller) setChatMemberCount(count int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getChatMemberCount = count
}

func (c *routeTestCaller) setChatAdminsResult(raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getChatAdminsResult = raw
}

func (c *routeTestCaller) setMethodError(method string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.errByMethod == nil {
		c.errByMethod = make(map[string]error)
	}
	c.errByMethod[method] = err
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
	case "answerCallbackQuery", "approveChatJoinRequest", "declineChatJoinRequest":
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

type routeTestStore struct {
	routeTestStoreStub

	mu                    sync.Mutex
	saveOAuthStateCalls   int
	savedOAuthStateCalls  []core.OAuthStatePayload
	deleteOAuthStateCalls int
	viewerIdentity        core.UserIdentity
	viewerIdentityOK      bool
	ownedCreator          core.Creator
	ownedCreatorOK        bool
	managedGroupsByChatID map[int64]core.ManagedGroup
	trackedMembersByGroup map[int64]map[int64]bool
	untrackedUpserts      []routeTestUntrackedUpsert
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

func (s *routeTestStore) SaveOAuthState(_ context.Context, _ string, payload core.OAuthStatePayload, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveOAuthStateCalls++
	s.savedOAuthStateCalls = append(s.savedOAuthStateCalls, payload)
	return nil
}

func (s *routeTestStore) DeleteOAuthState(_ context.Context, _ string) (core.OAuthStatePayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteOAuthStateCalls++
	return core.OAuthStatePayload{}, nil
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

func (s *routeTestStore) ManagedGroupByChatID(_ context.Context, chatID int64) (core.ManagedGroup, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, ok := s.managedGroupsByChatID[chatID]
	return group, ok, nil
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
	s.untrackedUpserts = append(s.untrackedUpserts, routeTestUntrackedUpsert{
		chatID:         chatID,
		telegramUserID: telegramUserID,
		source:         source,
		status:         status,
	})
	return nil
}

func routeTestAdminMemberJSON(userID int64, isBot bool, canInviteUsers bool, canRestrictMembers bool) json.RawMessage {
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
func (routeTestStoreStub) UpsertManagedGroup(context.Context, core.ManagedGroup) error { return nil }
func (routeTestStoreStub) DeleteManagedGroup(context.Context, int64) error             { return nil }
func (routeTestStoreStub) AddTrackedGroupMember(context.Context, int64, int64, string, time.Time) error {
	return nil
}
func (routeTestStoreStub) RemoveTrackedGroupMember(context.Context, int64, int64) error { return nil }
func (routeTestStoreStub) IsTrackedGroupMember(context.Context, int64, int64) (bool, error) {
	return false, nil
}
func (routeTestStoreStub) UpsertUntrackedGroupMember(context.Context, int64, int64, string, string, time.Time) error {
	return nil
}
func (routeTestStoreStub) RemoveUntrackedGroupMember(context.Context, int64, int64) error { return nil }
func (routeTestStoreStub) CountUntrackedGroupMembers(context.Context, int64) (int, error) {
	return 0, nil
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
func (routeTestStoreStub) DeleteCreatorData(context.Context, int64) (int, []string, error) {
	return 0, nil, nil
}
func (routeTestStoreStub) UpdateCreatorTokens(context.Context, string, string, string) error {
	return nil
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
