package mtproto

import (
	"errors"
	"testing"

	"github.com/gotd/td/telegram/peers/members"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		appID   int
		appHash string
		session string
		wantErr error
	}{
		{name: "invalid app id", appHash: "hash", session: "c2Vzc2lvbg==", wantErr: errInvalidAppID},
		{name: "missing app hash", appID: 1, session: "c2Vzc2lvbg==", wantErr: errAppHashRequired},
		{name: "empty session", appID: 1, appHash: "hash", session: "", wantErr: errSessionEmpty},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := New(tc.appID, tc.appHash, tc.session)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New() error = %v, want errors.Is(_, %v)=true", err, tc.wantErr)
			}
		})
	}
}

func TestStageExtractsWrappedStageError(t *testing.T) {
	t.Parallel()

	stage, ok := Stage(errors.Join(errors.New("wrapper"), StageError{Stage: "join_failed", Err: errors.New("boom")}))
	if !ok || stage != "join_failed" {
		t.Fatalf("Stage() = (%q, %v), want (%q, true)", stage, ok, "join_failed")
	}
}

func TestRoleFromStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status      members.Status
		wantRole    MemberRole
		wantInclude bool
	}{
		{status: members.Plain, wantRole: MemberRoleMember, wantInclude: true},
		{status: members.Creator, wantRole: MemberRoleCreator, wantInclude: true},
		{status: members.Admin, wantRole: MemberRoleAdmin, wantInclude: true},
		{status: members.Banned, wantRole: MemberRoleRestricted, wantInclude: false},
		{status: members.Left, wantRole: MemberRoleRestricted, wantInclude: false},
	}

	for _, tc := range tests {
		gotRole, gotInclude := roleFromStatus(tc.status)
		if gotRole != tc.wantRole || gotInclude != tc.wantInclude {
			t.Fatalf("roleFromStatus(%v) = (%q, %v), want (%q, %v)", tc.status, gotRole, gotInclude, tc.wantRole, tc.wantInclude)
		}
	}
}

func TestStatusTextDefaultsToMember(t *testing.T) {
	t.Parallel()

	if got := StatusText(Member{}); got != string(MemberRoleMember) {
		t.Fatalf("StatusText(Member{}) = %q, want %q", got, MemberRoleMember)
	}
}
