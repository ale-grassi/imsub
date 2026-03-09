package flows

import "testing"

func TestParseCallbackAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want callbackAction
		ok   bool
	}{
		{
			name: "viewer refresh",
			data: viewerRefreshCallback(),
			want: callbackAction{domain: callbackDomainViewer, verb: callbackVerbRefresh},
			ok:   true,
		},
		{
			name: "creator reconnect",
			data: creatorReconnectCallback(),
			want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbReconnect},
			ok:   true,
		},
		{
			name: "creator open groups",
			data: creatorManageGroupsCallback(),
			want: callbackAction{
				domain: callbackDomainCreator,
				verb:   callbackVerbOpen,
				target: creatorCallbackTargetGroups,
			},
			ok: true,
		},
		{
			name: "creator pick group",
			data: creatorGroupPickCallback(-100123),
			want: callbackAction{
				domain: callbackDomainCreator,
				verb:   callbackVerbPick,
				target: creatorCallbackTargetGroup,
				chatID: -100123,
			},
			ok: true,
		},
		{
			name: "creator execute group",
			data: creatorGroupExecuteCallback(-100123),
			want: callbackAction{
				domain: callbackDomainCreator,
				verb:   callbackVerbExecute,
				target: creatorCallbackTargetGroup,
				chatID: -100123,
			},
			ok: true,
		},
		{
			name: "creator menu",
			data: creatorMenuCallback(),
			want: callbackAction{
				domain: callbackDomainCreator,
				verb:   callbackVerbMenu,
			},
			ok: true,
		},
		{
			name: "reset pick",
			data: resetPickCallback(resetOriginCreator, resetScopeBoth),
			want: callbackAction{
				domain: callbackDomainReset,
				verb:   callbackVerbPick,
				origin: resetOriginCreator,
				scope:  resetScopeBoth,
			},
			ok: true,
		},
		{
			name: "reset cancel",
			data: resetCancelCallback(resetOriginCommand),
			want: callbackAction{
				domain: callbackDomainReset,
				verb:   callbackVerbCancel,
				origin: resetOriginCommand,
			},
			ok: true,
		},
		{
			name: "reset menu",
			data: resetMenuCallback(resetOriginViewer),
			want: callbackAction{
				domain: callbackDomainReset,
				verb:   callbackVerbMenu,
				origin: resetOriginViewer,
			},
			ok: true,
		},
		{name: "missing parts", data: "reset", ok: false},
		{name: "invalid origin", data: "reset:open:oops", ok: false},
		{name: "invalid scope", data: "reset:pick:viewer:oops", ok: false},
		{name: "invalid creator verb", data: "creator:oops", ok: false},
		{name: "invalid creator open target", data: "creator:open:oops", ok: false},
		{name: "invalid creator pick id", data: "creator:pick:group:oops", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseCallbackAction(tt.data)
			if ok != tt.ok {
				t.Fatalf("parseCallbackAction(%q) ok = %t, want %t", tt.data, ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if got != tt.want {
				t.Fatalf("parseCallbackAction(%q) = %+v, want %+v", tt.data, got, tt.want)
			}
		})
	}
}
