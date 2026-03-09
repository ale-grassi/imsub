package flows

import (
	"errors"
	"testing"

	"imsub/internal/core"

	"github.com/mymmrac/telego"
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

func TestRegisterTelegramHandlersStartCommandSendFailureInvalidatesOAuthState(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.caller.setMethodError("sendMessage", errors.New("telegram down"))

	h.handleUpdate(t, telego.Update{
		UpdateID: 100,
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

	if got := h.store.saveOAuthStateCallCount(); got != 1 {
		t.Fatalf("SaveOAuthState call count = %d, want 1", got)
	}
	if got := h.store.deleteOAuthStateCallCount(); got != 1 {
		t.Fatalf("DeleteOAuthState call count = %d, want 1", got)
	}
	h.caller.assertExactMethods(t, "sendMessage")
}

func TestRegisterTelegramHandlersRefreshViewerCallback(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 3,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-1",
			Data: viewerRefreshCallback(),
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
			Data: creatorReconnectCallback(),
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

func TestRegisterTelegramHandlersResetViewerOriginBackReturnsViewerMenu(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setViewerIdentity(core.UserIdentity{
		TelegramUserID: 55,
		TwitchUserID:   "viewer-1",
		TwitchLogin:    "viewer_login",
	})
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		Name:            "streamer",
		OwnerTelegramID: 55,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 34,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reset-viewer",
			Data: resetOpenCallback(resetOriginViewer),
			From: telego.User{
				ID:           55,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   55,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, resetPickCallback(resetOriginViewer, resetScopeViewer))
	h.assertEditMessageHasCallback(t, body, resetMenuCallback(resetOriginViewer))

	h.handleUpdate(t, telego.Update{
		UpdateID: 35,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reset-viewer-back",
			Data: resetMenuCallback(resetOriginViewer),
			From: telego.User{
				ID:           55,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   55,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, viewerRefreshCallback())
	h.assertEditMessageLacksCallback(t, body, creatorRefreshCallback())
}

func TestRegisterTelegramHandlersResetCreatorOriginBackReturnsCreatorMenu(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setViewerIdentity(core.UserIdentity{
		TelegramUserID: 77,
		TwitchUserID:   "viewer-1",
		TwitchLogin:    "viewer_login",
	})
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		Name:            "streamer",
		OwnerTelegramID: 77,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 36,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reset-creator",
			Data: resetOpenCallback(resetOriginCreator),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 91,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, resetPickCallback(resetOriginCreator, resetScopeViewer))
	h.assertEditMessageHasCallback(t, body, resetMenuCallback(resetOriginCreator))

	h.handleUpdate(t, telego.Update{
		UpdateID: 37,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reset-creator-back",
			Data: resetMenuCallback(resetOriginCreator),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 91,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorRefreshCallback())
	h.assertEditMessageLacksCallback(t, body, viewerRefreshCallback())
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

func TestRegisterTelegramHandlersRegisterGroupBlocksWhenBotLacksRequiredPermissions(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		Name:            "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setBotUserID(999)
	h.caller.setChatMember(77, routeTestAdminMemberJSON(77, false, true, true))
	h.caller.setChatMember(999, routeTestAdminMemberJSON(999, true, false, false))

	h.handleUpdate(t, telego.Update{
		UpdateID: 6,
		Message: &telego.Message{
			MessageID: 12,
			Text:      "/registergroup",
			Chat: telego.Chat{
				ID:    -10077,
				Type:  telego.ChatTypeSupergroup,
				Title: "VIP",
			},
			From: &telego.User{
				ID:           77,
				LanguageCode: "en",
			},
		},
	})

	h.caller.assertExactMethods(t, "getChatMember", "getMe", "getChatMember", "sendMessage")
}

func TestRegisterTelegramHandlersUnregisterGroupCommand(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		Name:            "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -10077,
		CreatorID: "creator-1",
		GroupName: "VIP",
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 61,
		Message: &telego.Message{
			MessageID: 13,
			Text:      "/unregistergroup",
			Chat: telego.Chat{
				ID:    -10077,
				Type:  telego.ChatTypeSupergroup,
				Title: "VIP",
			},
			From: &telego.User{
				ID:           77,
				LanguageCode: "en",
			},
		},
	})

	h.caller.assertExactMethods(t, "sendMessage")
}

func TestRegisterTelegramHandlersChatMemberJoinTracksUntrackedUser(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setManagedGroup(core.ManagedGroup{ChatID: -10033, CreatorID: "creator-1", GroupName: "VIP"})

	h.handleUpdate(t, telego.Update{
		UpdateID: 7,
		ChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10033, Type: telego.ChatTypeSupergroup},
			From: telego.User{ID: 700, IsBot: false},
			OldChatMember: &telego.ChatMemberLeft{
				Status: telego.MemberStatusLeft,
				User:   telego.User{ID: 700, IsBot: false},
			},
			NewChatMember: &telego.ChatMemberMember{
				Status: telego.MemberStatusMember,
				User:   telego.User{ID: 700, IsBot: false},
			},
		},
	})

	if got := h.store.lastUntrackedMemberUpsert(); got.telegramUserID != 700 || got.source != "chat_member" {
		t.Fatalf("last untracked upsert = %+v, want telegram_user_id=700 source=chat_member", got)
	}
}

func TestRegisterTelegramHandlersGroupMessageTracksUntrackedUser(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setManagedGroup(core.ManagedGroup{ChatID: -10044, CreatorID: "creator-1", GroupName: "VIP"})

	h.handleUpdate(t, telego.Update{
		UpdateID: 8,
		Message: &telego.Message{
			MessageID: 30,
			Text:      "hello",
			Chat: telego.Chat{
				ID:   -10044,
				Type: telego.ChatTypeSupergroup,
			},
			From: &telego.User{
				ID:    701,
				IsBot: false,
			},
		},
	})

	if got := h.store.lastUntrackedMemberUpsert(); got.telegramUserID != 701 || got.source != "message" {
		t.Fatalf("last untracked upsert = %+v, want telegram_user_id=701 source=message", got)
	}
}
