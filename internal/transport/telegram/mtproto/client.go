// Package mtproto wraps the operator-managed Telegram user session used for
// one-shot bootstrap membership dumps during group registration.
package mtproto

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/peers/members"
)

var errUnauthorizedSession = errors.New("mtproto session is not authorized")

var (
	errInvalidAppID          = errors.New("invalid mtproto app id")
	errAppHashRequired       = errors.New("mtproto app hash is required")
	errSessionEmpty          = errors.New("mtproto session is empty")
	errNilClient             = errors.New("mtproto client is nil")
	errUnsupportedJoinedPeer = errors.New("unsupported joined peer type")
	errRunClient             = errors.New("run mtproto client")
	errListChannelMembers    = errors.New("list channel members")
	errListChatMembers       = errors.New("list chat members")
)

// MemberRole classifies a dumped Telegram member.
type MemberRole string

// Dumped member roles.
const (
	MemberRoleMember     MemberRole = "member"
	MemberRoleRestricted MemberRole = "restricted"
	MemberRoleAdmin      MemberRole = "admin"
	MemberRoleCreator    MemberRole = "creator"
)

// Member is one Telegram user observed through MTProto.
type Member struct {
	TelegramUserID int64
	Username       string
	DisplayName    string
	IsBot          bool
	Role           MemberRole
}

// Client wraps one operator-managed MTProto user session.
type Client struct {
	appID       int
	appHash     string
	sessionData []byte
	selfUserID  int64
}

// New builds an MTProto client wrapper from config values.
func New(appID int, appHash, sessionString string) (*Client, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("%w: %d", errInvalidAppID, appID)
	}
	if strings.TrimSpace(appHash) == "" {
		return nil, errAppHashRequired
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sessionString))
	if err != nil {
		return nil, fmt.Errorf("decode mtproto session: %w", err)
	}
	if len(raw) == 0 {
		return nil, errSessionEmpty
	}
	return &Client{
		appID:       appID,
		appHash:     strings.TrimSpace(appHash),
		sessionData: raw,
	}, nil
}

// Validate confirms the session is authorized and returns the MTProto service user id.
func (c *Client) Validate(ctx context.Context) (int64, error) {
	if c == nil {
		return 0, errNilClient
	}
	var selfUserID int64
	if err := c.run(ctx, func(ctx context.Context, client *telegram.Client, _ *peers.Manager) error {
		self, err := client.Self(ctx)
		if err != nil {
			return fmt.Errorf("load self: %w", err)
		}
		selfUserID = self.ID
		return nil
	}); err != nil {
		return 0, err
	}
	c.selfUserID = selfUserID
	return selfUserID, nil
}

// SelfUserID returns the validated MTProto service user id.
func (c *Client) SelfUserID() int64 {
	if c == nil {
		return 0
	}
	return c.selfUserID
}

// DumpMembersViaInvite joins a chat using the provided invite link if needed,
// lists members, and keeps the MTProto user in the group.
func (c *Client) DumpMembersViaInvite(ctx context.Context, inviteLink string) ([]Member, error) {
	if c == nil {
		return nil, errNilClient
	}
	var dumped []Member
	if err := c.run(ctx, func(ctx context.Context, _ *telegram.Client, manager *peers.Manager) error {
		peer, err := manager.JoinLink(ctx, inviteLink)
		if err != nil {
			return StageError{Stage: "join_failed", Err: err}
		}
		membersDump, listErr := dumpPeerMembers(ctx, peer)
		if listErr != nil {
			return StageError{Stage: "list_failed", Err: listErr}
		}
		dumped = membersDump
		return nil
	}); err != nil {
		return nil, err
	}
	return dumped, nil
}

// DumpMembersByChatID lists members for a chat already known to the MTProto session.
func (c *Client) DumpMembersByChatID(ctx context.Context, chatID int64) ([]Member, error) {
	if c == nil {
		return nil, errNilClient
	}
	var dumped []Member
	if err := c.run(ctx, func(ctx context.Context, _ *telegram.Client, manager *peers.Manager) error {
		peer, err := manager.ResolveTDLibID(ctx, constant.TDLibPeerID(chatID))
		if err != nil {
			return StageError{Stage: "resolve_failed", Err: err}
		}
		membersDump, listErr := dumpPeerMembers(ctx, peer)
		if listErr != nil {
			return StageError{Stage: "list_failed", Err: listErr}
		}
		dumped = membersDump
		return nil
	}); err != nil {
		return nil, err
	}
	return dumped, nil
}

// StageError reports the bootstrap stage that failed.
type StageError struct {
	Stage string
	Err   error
}

func (e StageError) Error() string {
	return fmt.Sprintf("%s: %v", e.Stage, e.Err)
}

func (e StageError) Unwrap() error {
	return e.Err
}

// Stage returns the normalized MTProto bootstrap stage for err, if any.
func Stage(err error) (string, bool) {
	var stageErr StageError
	if !errors.As(err, &stageErr) {
		return "", false
	}
	return stageErr.Stage, true
}

func (c *Client) run(ctx context.Context, fn func(context.Context, *telegram.Client, *peers.Manager) error) error {
	storage := &session.StorageMemory{}
	if err := storage.StoreSession(ctx, c.sessionData); err != nil {
		return fmt.Errorf("prime mtproto session storage: %w", err)
	}

	client := telegram.NewClient(c.appID, c.appHash, telegram.Options{
		SessionStorage: storage,
	})
	if err := client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("load mtproto auth status: %w", err)
		}
		if !status.Authorized {
			return errUnauthorizedSession
		}

		manager := peers.Options{}.Build(client.API())
		if err := manager.Init(ctx); err != nil {
			return fmt.Errorf("init mtproto peer manager: %w", err)
		}
		return fn(ctx, client, manager)
	}); err != nil {
		return fmt.Errorf("%w: %w", errRunClient, err)
	}
	return nil
}

func dumpPeerMembers(ctx context.Context, peer peers.Peer) ([]Member, error) {
	membersOut := make([]Member, 0, 64)
	appendMember := func(user peers.User, role MemberRole) {
		username, _ := user.Username()
		_, isBot := user.ToBot()
		membersOut = append(membersOut, Member{
			TelegramUserID: user.ID(),
			Username:       username,
			DisplayName:    user.VisibleName(),
			IsBot:          isBot,
			Role:           role,
		})
	}

	switch p := peer.(type) {
	case peers.Channel:
		if err := members.Channel(p).ForEach(ctx, func(member members.Member) error {
			role, include := roleFromStatus(member.Status())
			if include {
				appendMember(member.User(), role)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("%w: %w", errListChannelMembers, err)
		}
	case peers.Chat:
		if err := members.Chat(p).ForEach(ctx, func(member members.Member) error {
			role, include := roleFromStatus(member.Status())
			if include {
				appendMember(member.User(), role)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("%w: %w", errListChatMembers, err)
		}
	default:
		return nil, fmt.Errorf("%w: %T", errUnsupportedJoinedPeer, peer)
	}
	return membersOut, nil
}

func roleFromStatus(status members.Status) (MemberRole, bool) {
	switch status {
	case members.Plain:
		return MemberRoleMember, true
	case members.Creator:
		return MemberRoleCreator, true
	case members.Admin:
		return MemberRoleAdmin, true
	case members.Banned, members.Left:
		return MemberRoleRestricted, false
	default:
		return MemberRoleMember, false
	}
}

// StatusText normalizes a dumped member role into the persistence status field.
func StatusText(member Member) string {
	if member.Role == "" {
		return string(MemberRoleMember)
	}
	return string(member.Role)
}
