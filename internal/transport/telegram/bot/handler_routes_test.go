package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"imsub/internal/core"

	"github.com/mymmrac/telego"
)

type answerCallbackRequest struct {
	Text      string `json:"text"`
	ShowAlert bool   `json:"show_alert"`
}

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

func TestRegisterTelegramHandlersCreatorManageGroupsFlow(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1001,
		CreatorID: "creator-1",
		GroupName: "VIP One",
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1002,
		CreatorID: "creator-1",
		GroupName: "VIP Two",
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 34,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-groups-open",
			Data: creatorManageGroupsCallback(),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupPickCallback(-1001))
	h.assertEditMessageHasCallback(t, body, creatorGroupPickCallback(-1002))
	h.assertEditMessageHasCallback(t, body, creatorMenuCallback())
	h.assertEditMessageTextContains(t, body, "Manage linked groups")

	h.handleUpdate(t, telego.Update{
		UpdateID: 35,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-groups-pick",
			Data: creatorGroupPickCallback(-1001),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyOpenCallback(-1001))
	h.assertEditMessageHasCallback(t, body, creatorGroupConfirmCallback(-1001))
	h.assertEditMessageHasCallback(t, body, creatorGroupBackCallback())
	h.assertEditMessageTextContains(t, body, "Group settings")
	h.assertEditMessageTextContains(t, body, "VIP One")

	h.handleUpdate(t, telego.Update{
		UpdateID: 36,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-group-policy-open",
			Data: creatorGroupPolicyOpenCallback(-1001),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyPickCallback(-1001, core.GroupPolicyObserve))
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyPickCallback(-1001, core.GroupPolicyObserveWarn))
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyPickCallback(-1001, core.GroupPolicyKick))
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyPickCallback(-1001, core.GroupPolicyGraceWeek))
	h.assertEditMessageHasCallback(t, body, creatorGroupPickCallback(-1001))
	h.assertEditMessageTextContains(t, body, "Change group policy")

	h.handleUpdate(t, telego.Update{
		UpdateID: 37,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-groups-confirm-open",
			Data: creatorGroupConfirmCallback(-1001),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupExecuteWithActionCallback(-1001, core.CreatorResetKeepMembers))
	h.assertEditMessageHasCallback(t, body, creatorGroupExecuteWithActionCallback(-1001, core.CreatorResetKickTrackedMembers))
	h.assertEditMessageHasCallback(t, body, creatorGroupPickCallback(-1001))
	h.assertEditMessageTextContains(t, body, "Unlink this group?")

	h.handleUpdate(t, telego.Update{
		UpdateID: 38,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-groups-exec",
			Data: creatorGroupExecuteWithActionCallback(-1001, core.CreatorResetKeepMembers),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 90,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageLacksCallback(t, body, creatorGroupPickCallback(-1001))
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyOpenCallback(-1002))
	h.assertEditMessageHasCallback(t, body, creatorGroupConfirmCallback(-1002))
	h.assertEditMessageHasCallback(t, body, creatorMenuCallback())
	h.assertEditMessageTextContains(t, body, "VIP Two")
	if h.store.hasManagedGroup(-1001) {
		t.Fatal("managed group -1001 still present after creator menu unregister")
	}
}

func TestRegisterTelegramHandlersCreatorSingleGroupGoesStraightToSettings(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1001,
		CreatorID: "creator-1",
		GroupName: "VIP One",
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 37,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-single-group-open",
			Data: creatorManageGroupsCallback(),
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
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyOpenCallback(-1001))
	h.assertEditMessageHasCallback(t, body, creatorGroupConfirmCallback(-1001))
	h.assertEditMessageHasCallback(t, body, creatorMenuCallback())
	h.assertEditMessageLacksCallback(t, body, creatorGroupPickCallback(-1001))
	h.assertEditMessageTextContains(t, body, "Group settings")
	h.assertEditMessageTextContains(t, body, "VIP One")
}

func TestRegisterTelegramHandlersCreatorGroupPolicyUpdateFlow(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1003,
		CreatorID: "creator-1",
		GroupName: "VIP Policy",
		Policy:    core.GroupPolicyObserve,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 38,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-settings",
			Data: creatorGroupPickCallback(-1003),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 92,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 39,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-picker",
			Data: creatorGroupPolicyOpenCallback(-1003),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 92,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyPickCallback(-1003, core.GroupPolicyKick))

	h.handleUpdate(t, telego.Update{
		UpdateID: 40,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-confirm-open",
			Data: creatorGroupPolicyPickCallback(-1003, core.GroupPolicyKick),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 92,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyExecuteCallback(-1003, core.GroupPolicyKick))
	h.assertEditMessageTextContains(t, body, "Confirm group policy change")

	h.handleUpdate(t, telego.Update{
		UpdateID: 41,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-confirm",
			Data: creatorGroupPolicyExecuteCallback(-1003, core.GroupPolicyKick),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 92,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	group, ok, err := h.store.ManagedGroupByChatID(t.Context(), -1003)
	if err != nil || !ok {
		t.Fatalf("ManagedGroupByChatID() = %+v, %t, %v; want stored group", group, ok, err)
	}
	if group.Policy != core.GroupPolicyKick {
		t.Fatalf("group policy = %q, want %q", group.Policy, core.GroupPolicyKick)
	}
	body = h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyOpenCallback(-1003))
	h.assertEditMessageTextContains(t, body, "Group policy updated")
}

func TestRegisterTelegramHandlersGroupRegisterPolicyCallbackShowsWarningsInMessageNotAlert(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setBotUserID()
	h.caller.setChatMember(77, mustMarshalJSON(map[string]any{
		"status": "administrator",
		"user":   map[string]any{"id": 77, "is_bot": false, "first_name": "Owner"},
	}))
	h.caller.setChatMember(999, mustMarshalJSON(map[string]any{
		"status":               "administrator",
		"user":                 map[string]any{"id": 999, "is_bot": true, "first_name": "ImSub"},
		"can_invite_users":     false,
		"can_restrict_members": false,
	}))

	h.handleUpdate(t, telego.Update{
		UpdateID: 401,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-group-warnings",
			Data: groupRegisterPolicyCallback(-1007, 0, core.GroupPolicyGraceWeek),
			From: telego.User{ID: 77, LanguageCode: "en"},
			Message: &telego.Message{
				MessageID: 120,
				Chat: telego.Chat{
					ID:    -1007,
					Type:  telego.ChatTypeSupergroup,
					Title: "VIP Group",
				},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageTextContains(t, body, "Group settings need attention")
	h.assertEditMessageTextContains(t, body, "Invite Users")
	h.assertEditMessageTextContains(t, body, "Ban Users")

	callbackBody := h.caller.lastAnswerCallbackBody()
	var ack answerCallbackRequest
	if err := json.Unmarshal(callbackBody, &ack); err != nil {
		t.Fatalf("json.Unmarshal(answerCallbackQuery body) error = %v, body = %s", err, callbackBody)
	}
	if ack.Text != "" {
		t.Fatalf("answerCallbackQuery text = %q, want empty text when warnings are shown in message", ack.Text)
	}
	if ack.ShowAlert {
		t.Fatal("answerCallbackQuery show_alert = true, want false")
	}
}

func TestRegisterTelegramHandlersCreatorGroupPolicyUpdateNotManaged(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 42,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-not-managed",
			Data: creatorGroupPolicyExecuteCallback(-1999, core.GroupPolicyKick),
			From: telego.User{ID: 77, LanguageCode: "en"},
			Message: &telego.Message{
				MessageID: 93,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageTextContains(t, body, "no longer linked")
}

func TestRegisterTelegramHandlersCreatorGroupPolicyUpdateNotOwner(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1004,
		CreatorID: "creator-2",
		GroupName: "Foreign",
		Policy:    core.GroupPolicyObserve,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 43,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-not-owner",
			Data: creatorGroupPolicyExecuteCallback(-1004, core.GroupPolicyKick),
			From: telego.User{ID: 77, LanguageCode: "en"},
			Message: &telego.Message{
				MessageID: 94,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageTextContains(t, body, "can change its policy")
}

func TestRegisterTelegramHandlersCreatorGroupPolicyUpdateUnchanged(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1005,
		CreatorID: "creator-1",
		GroupName: "VIP Same",
		Policy:    core.GroupPolicyObserveWarn,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 44,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-policy-unchanged",
			Data: creatorGroupPolicyExecuteCallback(-1005, core.GroupPolicyObserveWarn),
			From: telego.User{ID: 77, LanguageCode: "en"},
			Message: &telego.Message{
				MessageID: 95,
				Chat:      telego.Chat{ID: 77, Type: telego.ChatTypePrivate},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageTextContains(t, body, "No change")
	h.assertEditMessageHasCallback(t, body, creatorGroupPolicyOpenCallback(-1005))
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
		TwitchLogin:     "streamer",
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
		TwitchLogin:     "streamer",
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

func TestRegisterTelegramHandlersResetCreatorShowsMemberActionPickerWhenManagedGroupsExist(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1001,
		CreatorID: "creator-1",
		GroupName: "VIP",
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 38,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reset-creator-pick",
			Data: resetPickCallback(resetOriginCreator, resetScopeCreator),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 92,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, resetActionPickCallback(resetOriginCreator, resetScopeCreator, core.CreatorResetKeepMembers))
	h.assertEditMessageHasCallback(t, body, resetActionPickCallback(resetOriginCreator, resetScopeCreator, core.CreatorResetKickTrackedMembers))
}

func TestRegisterTelegramHandlersResetCreatorWithoutGroupsSkipsMemberActionPicker(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 39,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-reset-creator-pick-no-groups",
			Data: resetPickCallback(resetOriginCreator, resetScopeCreator),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 93,
				Chat: telego.Chat{
					ID:   77,
					Type: telego.ChatTypePrivate,
				},
			},
		},
	})

	body := h.caller.lastEditMessageBody()
	h.assertEditMessageHasCallback(t, body, resetExecuteCallback(resetOriginCreator, resetScopeCreator))
	h.assertEditMessageLacksCallback(t, body, resetActionPickCallback(resetOriginCreator, resetScopeCreator, core.CreatorResetKeepMembers))
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

func TestRegisterTelegramHandlersApprovesJoinRequestInForumSupergroup(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 40,
		ChatJoinRequest: &telego.ChatJoinRequest{
			Chat: telego.Chat{
				ID:      -1003,
				Type:    telego.ChatTypeSupergroup,
				IsForum: true,
			},
			From: telego.User{ID: 101},
			InviteLink: &telego.ChatInviteLink{
				Name: "imsub-101-creator",
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

func TestRegisterTelegramHandlersDeclinesBlockedJoinRequest(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:                   "creator-1",
		OwnerTelegramID:      77,
		BlocklistSyncEnabled: true,
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -1005,
		CreatorID: "creator-1",
		GroupName: "VIP",
	})
	h.store.setViewerIdentity(core.UserIdentity{
		TelegramUserID: 102,
		TwitchUserID:   "tw-102",
		TwitchLogin:    "viewer102",
	})
	h.store.setCreatorBlocked("creator-1", "tw-102")

	h.handleUpdate(t, telego.Update{
		UpdateID: 42,
		ChatJoinRequest: &telego.ChatJoinRequest{
			Chat: telego.Chat{ID: -1005},
			From: telego.User{ID: 102},
			InviteLink: &telego.ChatInviteLink{
				Name: "imsub-102-creator",
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
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setBotUserID()
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

func TestRegisterTelegramHandlersRegisterGroupRepliesInSameForumTopic(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setChatMemberCount(2)
	h.caller.setChatAdministrators(json.RawMessage(`[
		{"status":"administrator","user":{"id":77,"is_bot":false,"first_name":"Admin"}},
		{"status":"administrator","user":{"id":999,"is_bot":true,"first_name":"ImSub"}}
	]`))
	h.caller.setChatMember(77, routeTestAdminMemberJSON(77, false, true, true))

	h.handleUpdate(t, telego.Update{
		UpdateID: 41,
		Message: &telego.Message{
			MessageID:       12,
			MessageThreadID: 321,
			IsTopicMessage:  true,
			Text:            "/registergroup",
			Chat: telego.Chat{
				ID:      -1004,
				Type:    telego.ChatTypeSupergroup,
				Title:   "VIP Forum",
				IsForum: true,
			},
			From: &telego.User{
				ID:           77,
				LanguageCode: "en",
			},
		},
	})

	var body map[string]any
	if err := json.Unmarshal(h.caller.lastSendMessageBody(), &body); err != nil {
		t.Fatalf("json.Unmarshal(sendMessage body) error = %v", err)
	}
	if got := body["message_thread_id"]; got != float64(321) {
		t.Fatalf("sendMessage message_thread_id = %v, want 321", got)
	}
}

func TestRegisterTelegramHandlersRegisterGroupAlwaysPromptsForPolicy(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setBotUserID()
	h.caller.setChatMemberCount(2)
	h.caller.setChatAdministrators(json.RawMessage(`[
		{"status":"administrator","user":{"id":77,"is_bot":false,"first_name":"Admin"}},
		{"status":"administrator","user":{"id":999,"is_bot":true,"first_name":"ImSub"}}
	]`))
	h.caller.setChatMember(77, routeTestAdminMemberJSON(77, false, true, true))
	h.caller.setChatMember(999, routeTestAdminMemberJSON(999, true, true, true))

	h.handleUpdate(t, telego.Update{
		UpdateID: 49,
		Message: &telego.Message{
			MessageID: 12,
			Text:      "/registergroup",
			Chat: telego.Chat{
				ID:    -1005,
				Type:  telego.ChatTypeSupergroup,
				Title: "VIP",
			},
			From: &telego.User{
				ID:           77,
				LanguageCode: "en",
			},
		},
	})

	body := h.caller.lastSendMessageBody()
	h.assertEditMessageTextContains(t, body, "Choose a group policy")
	h.assertEditMessageLacksCallback(t, body, creatorRefreshCallback())
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1005, 0, core.GroupPolicyObserve))
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1005, 0, core.GroupPolicyObserveWarn))
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1005, 0, core.GroupPolicyKick))
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1005, 0, core.GroupPolicyGraceWeek))
	if h.store.hasManagedGroup(-1005) {
		t.Fatal("group should not be registered before choosing a policy")
	}
}

func TestRegisterTelegramHandlersRegisterGroupPromptIncludesExistingMemberWarningWhenNeeded(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setBotUserID()
	h.caller.setChatMemberCount(6)
	h.caller.setChatAdministrators(json.RawMessage(`[
		{"status":"administrator","user":{"id":77,"is_bot":false,"first_name":"Admin"}},
		{"status":"administrator","user":{"id":999,"is_bot":true,"first_name":"ImSub"}}
	]`))
	h.caller.setChatMember(77, routeTestAdminMemberJSON(77, false, true, true))
	h.caller.setChatMember(999, routeTestAdminMemberJSON(999, true, true, true))

	h.handleUpdate(t, telego.Update{
		UpdateID: 50,
		Message: &telego.Message{
			MessageID: 12,
			Text:      "/registergroup",
			Chat: telego.Chat{
				ID:    -1006,
				Type:  telego.ChatTypeSupergroup,
				Title: "VIP",
			},
			From: &telego.User{
				ID:           77,
				LanguageCode: "en",
			},
		},
	})

	body := h.caller.lastSendMessageBody()
	h.assertEditMessageTextContains(t, body, "Choose a group policy")
	h.assertEditMessageTextContains(t, body, "4 existing non-admin members detected")
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1006, 0, core.GroupPolicyObserve))
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1006, 0, core.GroupPolicyObserveWarn))
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1006, 0, core.GroupPolicyKick))
	h.assertEditMessageHasCallback(t, body, groupRegisterPolicyCallback(-1006, 0, core.GroupPolicyGraceWeek))
	if h.store.hasManagedGroup(-1006) {
		t.Fatal("group should not be registered before choosing a policy")
	}
}

func TestRegisterTelegramHandlersRegisterGroupPolicyCallbackRegistersGroup(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.caller.setBotUserID()
	h.caller.setChatMember(77, routeTestAdminMemberJSON(77, false, true, true))
	h.caller.setChatMember(999, routeTestAdminMemberJSON(999, true, true, true))

	h.handleUpdate(t, telego.Update{
		UpdateID: 51,
		CallbackQuery: &telego.CallbackQuery{
			ID:   "cb-group-policy",
			Data: groupRegisterPolicyCallback(-1007, 0, core.GroupPolicyGraceWeek),
			From: telego.User{
				ID:           77,
				LanguageCode: "en",
			},
			Message: &telego.Message{
				MessageID: 44,
				Chat: telego.Chat{
					ID:    -1007,
					Type:  telego.ChatTypeSupergroup,
					Title: "VIP",
				},
			},
		},
	})

	group, ok, err := h.store.ManagedGroupByChatID(t.Context(), -1007)
	if err != nil || !ok {
		t.Fatalf("ManagedGroupByChatID() = %+v, %t, %v; want stored group", group, ok, err)
	}
	if group.Policy != core.GroupPolicyGraceWeek {
		t.Fatalf("group policy = %q, want %q", group.Policy, core.GroupPolicyGraceWeek)
	}
	if body := h.caller.lastEditMessageBody(); len(body) == 0 {
		t.Fatal("expected registration callback to edit the policy message")
	}
}

func TestRegisterTelegramHandlersUnregisterGroupCommand(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
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
	body := h.caller.lastSendMessageBody()
	h.assertSendMessageHasCallback(t, body, groupUnregisterExecuteCallback(-10077, core.CreatorResetKeepMembers))
	h.assertSendMessageHasCallback(t, body, groupUnregisterExecuteCallback(-10077, core.CreatorResetKickTrackedMembers))
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

func TestRegisterTelegramHandlersChatMemberJoinKicksObservedUnverifiedUserWhenPolicyIsKick(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -10034,
		CreatorID: "creator-1",
		GroupName: "VIP",
		Policy:    core.GroupPolicyKick,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 71,
		ChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10034, Type: telego.ChatTypeSupergroup},
			From: telego.User{ID: 702, IsBot: false},
			OldChatMember: &telego.ChatMemberLeft{
				Status: telego.MemberStatusLeft,
				User:   telego.User{ID: 702, IsBot: false},
			},
			NewChatMember: &telego.ChatMemberMember{
				Status: telego.MemberStatusMember,
				User:   telego.User{ID: 702, IsBot: false},
			},
		},
	})

	h.caller.assertExactMethods(t, "banChatMember", "unbanChatMember")
}

func TestRegisterTelegramHandlersChatMemberJoinWarnsInRegistrationThreadWhenPolicyIsObserveWarn(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setViewerIdentity(core.UserIdentity{
		TelegramUserID: 77,
		Language:       "it",
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:               -10035,
		CreatorID:            "creator-1",
		GroupName:            "VIP",
		Policy:               core.GroupPolicyObserveWarn,
		RegistrationThreadID: 321,
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 72,
		ChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10035, Type: telego.ChatTypeSupergroup},
			From: telego.User{ID: 703, IsBot: false},
			OldChatMember: &telego.ChatMemberLeft{
				Status: telego.MemberStatusLeft,
				User:   telego.User{ID: 703, IsBot: false},
			},
			NewChatMember: &telego.ChatMemberMember{
				Status: telego.MemberStatusMember,
				User:   telego.User{ID: 703, IsBot: false},
			},
		},
	})

	h.caller.assertExactMethods(t, "sendMessage")
	var body map[string]any
	if err := json.Unmarshal(h.caller.lastSendMessageBody(), &body); err != nil {
		t.Fatalf("json.Unmarshal(sendMessage body) error = %v", err)
	}
	if got := body["message_thread_id"]; got != float64(321) {
		t.Fatalf("sendMessage message_thread_id = %v, want 321", got)
	}
	if text, _ := body["text"].(string); !strings.Contains(text, "controllo accessi rigoroso") {
		t.Fatalf("sendMessage text = %q, want warning copy", text)
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

func TestRegisterTelegramHandlersMyChatMemberRemovalAutoUnregistersManagedGroup(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setViewerIdentity(core.UserIdentity{
		TelegramUserID: 77,
		Language:       "en",
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -10088,
		CreatorID: "creator-1",
		GroupName: "VIP",
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 90,
		MyChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10088, Type: telego.ChatTypeSupergroup, Title: "VIP"},
			From: telego.User{ID: 700, LanguageCode: "en"},
			OldChatMember: &telego.ChatMemberAdministrator{
				Status: telego.MemberStatusAdministrator,
				User:   telego.User{ID: 999, IsBot: true},
			},
			NewChatMember: &telego.ChatMemberLeft{
				Status: telego.MemberStatusLeft,
				User:   telego.User{ID: 999, IsBot: true},
			},
		},
	})

	if h.store.hasManagedGroup(-10088) {
		t.Fatal("managed group still present after bot removal, want auto-unregister")
	}
	h.caller.assertExactMethods(t, "sendMessage")
	body := parseSendMessageRequest(t, h.caller.lastSendMessageBody())
	if got := body.ChatID; got != 77 {
		t.Fatalf("sendMessage chat_id = %d, want owner DM chat_id 77", got)
	}
	if !strings.Contains(body.Text, "Group unlinked automatically") {
		t.Fatalf("sendMessage text = %q, want auto-unregister owner notice", body.Text)
	}
}

func TestRegisterTelegramHandlersMyChatMemberRemovalIgnoresUnmanagedGroup(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 91,
		MyChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10089, Type: telego.ChatTypeSupergroup, Title: "VIP"},
			From: telego.User{ID: 700, LanguageCode: "en"},
			OldChatMember: &telego.ChatMemberAdministrator{
				Status: telego.MemberStatusAdministrator,
				User:   telego.User{ID: 999, IsBot: true},
			},
			NewChatMember: &telego.ChatMemberLeft{
				Status: telego.MemberStatusLeft,
				User:   telego.User{ID: 999, IsBot: true},
			},
		},
	})

	h.caller.assertExactMethods(t)
}

func TestRegisterTelegramHandlersMyChatMemberRemovalCleanupLagStillNotifiesOwner(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarnessWithCleaner(t, routeTestCleaner{
		deleteEventSubsFn: func(_ context.Context, creatorID string) error {
			if creatorID != "creator-1" {
				t.Fatalf("DeleteEventSubsForCreator(%q), want creator-1", creatorID)
			}
			return errors.New("cleanup lag")
		},
	})
	h.store.setOwnedCreator(core.Creator{
		ID:              "creator-1",
		TwitchLogin:     "streamer",
		OwnerTelegramID: 77,
	})
	h.store.setViewerIdentity(core.UserIdentity{
		TelegramUserID: 77,
		Language:       "en",
	})
	h.store.setManagedGroup(core.ManagedGroup{
		ChatID:    -10090,
		CreatorID: "creator-1",
		GroupName: "VIP",
	})

	h.handleUpdate(t, telego.Update{
		UpdateID: 92,
		MyChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10090, Type: telego.ChatTypeSupergroup, Title: "VIP"},
			From: telego.User{ID: 700, LanguageCode: "en"},
			OldChatMember: &telego.ChatMemberAdministrator{
				Status: telego.MemberStatusAdministrator,
				User:   telego.User{ID: 999, IsBot: true},
			},
			NewChatMember: &telego.ChatMemberBanned{
				Status: telego.MemberStatusBanned,
				User:   telego.User{ID: 999, IsBot: true},
			},
		},
	})

	if h.store.hasManagedGroup(-10090) {
		t.Fatal("managed group still present after bot removal with cleanup lag, want auto-unregister")
	}
	h.caller.assertExactMethods(t, "sendMessage")
	body := parseSendMessageRequest(t, h.caller.lastSendMessageBody())
	if !strings.Contains(body.Text, "background cleanup tasks are still pending") {
		t.Fatalf("sendMessage text = %q, want cleanup-lag owner notice", body.Text)
	}
}

func TestRegisterTelegramHandlersMyChatMemberIgnoresNoStatusChange(t *testing.T) {
	t.Parallel()

	h := newRouteTestHarness(t)

	h.handleUpdate(t, telego.Update{
		UpdateID: 95,
		MyChatMember: &telego.ChatMemberUpdated{
			Chat: telego.Chat{ID: -10091, Type: telego.ChatTypeSupergroup, Title: "VIP"},
			From: telego.User{ID: 77, LanguageCode: "en"},
			OldChatMember: &telego.ChatMemberAdministrator{
				Status: telego.MemberStatusAdministrator,
				User:   telego.User{ID: 999, IsBot: true},
			},
			NewChatMember: &telego.ChatMemberAdministrator{
				Status: telego.MemberStatusAdministrator,
				User:   telego.User{ID: 999, IsBot: true},
			},
		},
	})

	if got := h.caller.lastSendMessageBody(); got != nil {
		t.Fatalf("sendMessage body = %s, want nil", got)
	}
}
