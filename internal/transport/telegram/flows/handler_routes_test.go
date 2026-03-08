package flows

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"imsub/internal/core"
	"imsub/internal/platform/config"
	"imsub/internal/platform/i18n"
	"imsub/internal/platform/ratelimit"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegohandler"
)

func TestRegisterTelegramHandlersStartCommand(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 1,
		Message: &telego.Message{
			MessageID: 10,
			Text:      "/start",
			Chat: telego.Chat{
				ID:   42,
				Type: telego.ChatTypePrivate,
			},
			From: &telego.User{
				ID:           42,
				FirstName:    "Viewer",
				LanguageCode: "en",
			},
		},
	})

	h.assertOAuthPromptSaved(t, 2, core.OAuthModeViewer, 42, 101)
	h.caller.assertExactMethods(t, "sendMessage")
}

func TestRegisterTelegramHandlersCreatorCommand(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 2,
		Message: &telego.Message{
			MessageID: 11,
			Text:      "/creator",
			Chat: telego.Chat{
				ID:   77,
				Type: telego.ChatTypePrivate,
			},
			From: &telego.User{
				ID:           77,
				LanguageCode: "en",
			},
		},
	})

	h.assertOAuthPromptSaved(t, 2, core.OAuthModeCreator, 77, 101)
	h.caller.assertExactMethods(t, "sendMessage")
}

func TestRegisterTelegramHandlersRefreshViewerCallback(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 3,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-1",
			Data: ui.ActionRefreshViewer,
			From: telego.User{
				ID:           55,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 44,
				Chat: telego.Chat{
					ID:   55,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	h.assertOAuthPromptSaved(t, 1, core.OAuthModeViewer, 55, 44)
	h.caller.assertExactMethods(t, "editMessageText", "answerCallbackQuery")
}

func TestRegisterTelegramHandlersReconnectCreatorCallback(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 33,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reconnect",
			Data: ui.ActionReconnectCreator,
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 88,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	h.assertOAuthPromptSaved(t, 1, core.OAuthModeCreator, 77, 88)
	if !h.store.lastSavedStatePayload().Reconnect {
		t.Fatal("last saved payload reconnect = false, want true")
	}
	h.caller.assertExactMethods(t, "editMessageText", "answerCallbackQuery")
}

func TestRegisterTelegramHandlersApprovesJoinRequest(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 4,
		ChatJoinRequest: &telego.ChatJoinRequest{
			Chat: telego.Chat{ID: -1001},
			From: telego.User{ID: 99},
			InviteLink: &telego.ChatInviteLink{
				Name: "imsub-99-creator",
			},
		},
	})

	h.caller.assertExactMethods(t, "approveChatJoinRequest")
}

func TestRegisterTelegramHandlersDeclinesMismatchedJoinRequest(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 5,
		ChatJoinRequest: &telego.ChatJoinRequest{
			Chat: telego.Chat{ID: -1002},
			From: telego.User{ID: 100},
			InviteLink: &telego.ChatInviteLink{
				Name: "imsub-99-creator",
			},
		},
	})

	h.caller.assertExactMethods(t, "declineChatJoinRequest")
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

	controller := New(Dependencies{
		Config: config.Config{
			PublicBaseURL: "https://example.com",
		},
		Store:           store,
		TelegramLimiter: limiter,
		TelegramBot:     bot,
		TelegramHandler: bh,
		Services: Services{
			Viewer:  core.NewViewer(store, routeTestGroupOps{}, nil),
			Creator: core.NewCreator(store, routeTestEventSubChecker{}, nil),
			Reset:   core.NewResetter(store, func(context.Context, int64, int64) error { return nil }, nil),
		},
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

type routeTestCaller struct {
	mu      sync.Mutex
	methods []string
}

func (c *routeTestCaller) Call(_ context.Context, url string, _ *telegoapi.RequestData) (*telegoapi.Response, error) {
	method := url[strings.LastIndex(url, "/")+1:]

	c.mu.Lock()
	c.methods = append(c.methods, method)
	c.mu.Unlock()

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
	default:
		return nil, fmt.Errorf("unexpected Telegram method %q", method)
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

type routeTestStore struct {
	routeTestStoreStub

	mu                   sync.Mutex
	saveOAuthStateCalls  int
	savedOAuthStateCalls []core.OAuthStatePayload
}

func (s *routeTestStore) saveOAuthStateCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveOAuthStateCalls
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
func (routeTestStoreStub) SaveUserCreator(context.Context, int64, string, string, string, string) (int64, error) {
	return 0, nil
}
func (routeTestStoreStub) UserCreatorIDs(context.Context, int64) ([]string, error) { return nil, nil }
func (routeTestStoreStub) RemoveUserCreatorByTelegram(context.Context, int64, string) error {
	return nil
}
func (routeTestStoreStub) AddUserCreatorMembership(context.Context, int64, string) error { return nil }
func (routeTestStoreStub) RemoveUserCreatorByTwitch(context.Context, string, string) (int64, bool, error) {
	return 0, false, nil
}
func (routeTestStoreStub) DeleteAllUserData(context.Context, int64) error { return nil }
func (routeTestStoreStub) Creator(context.Context, string) (core.Creator, bool, error) {
	return core.Creator{}, false, nil
}
func (routeTestStoreStub) ListCreators(context.Context) ([]core.Creator, error) { return nil, nil }
func (routeTestStoreStub) ListActiveCreators(context.Context) ([]core.Creator, error) {
	return nil, nil
}
func (routeTestStoreStub) OwnedCreatorForUser(context.Context, int64) (core.Creator, bool, error) {
	return core.Creator{}, false, nil
}
func (routeTestStoreStub) LoadCreatorsByIDs(context.Context, []string, func(core.Creator) bool) ([]core.Creator, error) {
	return nil, nil
}
func (routeTestStoreStub) UpsertCreator(context.Context, core.Creator) error { return nil }
func (routeTestStoreStub) DeleteCreatorData(context.Context, int64) (int, []string, error) {
	return 0, nil, nil
}
func (routeTestStoreStub) UpdateCreatorGroup(context.Context, string, int64, string) error {
	return nil
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
func (routeTestStoreStub) RepairUserCreatorReverseIndex(context.Context, []core.Creator) (
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

type routeTestGroupOps struct{}

func (routeTestGroupOps) IsGroupMember(context.Context, int64, int64) bool { return false }
func (routeTestGroupOps) CreateInviteLink(context.Context, int64, int64, string) (string, error) {
	return "", nil
}

type routeTestEventSubChecker struct{}

func (routeTestEventSubChecker) IsEventSubActiveForCreator(context.Context, string) (bool, error) {
	return false, nil
}
