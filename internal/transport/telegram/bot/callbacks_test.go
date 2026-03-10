package bot

import (
	"testing"

	"imsub/internal/core"
)

func TestParseCallbackAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want callbackAction
		ok   bool
	}{
		{name: "viewer refresh", in: "viewer:refresh", want: callbackAction{domain: callbackDomainViewer, verb: callbackVerbRefresh}, ok: true},
		{name: "creator reconnect", in: "creator:reconnect", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbReconnect}, ok: true},
		{name: "creator open groups", in: "creator:open:groups", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetGroups}, ok: true},
		{name: "creator open unregister confirm", in: "creator:open:group_confirm:123", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetGroupConfirm, chatID: 123}, ok: true},
		{name: "creator pick group", in: "creator:pick:group:123", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbPick, target: creatorCallbackTargetGroup, chatID: 123}, ok: true},
		{name: "creator open group policy", in: "creator:open:policy:123", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetPolicy, chatID: 123}, ok: true},
		{name: "creator pick group policy", in: "creator:pick:policy:observe_warn:123", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbPick, target: creatorCallbackTargetPolicy, policy: core.GroupPolicyObserveWarn, chatID: 123}, ok: true},
		{name: "creator execute group policy", in: "creator:exec:policy:grace_7d:123", want: callbackAction{domain: callbackDomainCreator, verb: callbackVerbExecute, target: creatorCallbackTargetPolicy, policy: core.GroupPolicyGraceWeek, chatID: 123}, ok: true},
		{name: "group pick policy", in: "group:pick:observe_warn:-100:321", want: callbackAction{domain: callbackDomainGroup, verb: callbackVerbPick, policy: core.GroupPolicyObserveWarn, chatID: -100, threadID: 321}, ok: true},
		{name: "reset pick both", in: "reset:pick:viewer:both", want: callbackAction{domain: callbackDomainReset, verb: callbackVerbPick, origin: resetOriginViewer, scope: resetScopeBoth}, ok: true},
		{name: "invalid domain", in: "other:refresh", ok: false},
		{name: "invalid creator target", in: "creator:open:other", ok: false},
		{name: "invalid creator policy", in: "creator:pick:policy:nope:123", ok: false},
		{name: "invalid reset scope", in: "reset:pick:viewer:nope", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseCallbackAction(tt.in)
			if ok != tt.ok {
				t.Fatalf("parseCallbackAction(%q) ok = %t, want %t", tt.in, ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if got != tt.want {
				t.Fatalf("parseCallbackAction(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}
