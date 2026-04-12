package usecase

import (
	"context"
	"fmt"
	"time"

	"imsub/internal/core"
	"imsub/internal/events"
	"imsub/internal/platform/i18n"
)

// UpdateGroupLanguageOutcome identifies the group-language update result.
type UpdateGroupLanguageOutcome string

const (
	// UpdateGroupLanguageOutcomeNotManaged means the group is not currently managed.
	UpdateGroupLanguageOutcomeNotManaged UpdateGroupLanguageOutcome = "not_managed"
	// UpdateGroupLanguageOutcomeNotOwner means the caller does not own the managed group.
	UpdateGroupLanguageOutcomeNotOwner UpdateGroupLanguageOutcome = "not_owner"
	// UpdateGroupLanguageOutcomeUnchanged means the selected language already matches the stored one.
	UpdateGroupLanguageOutcomeUnchanged UpdateGroupLanguageOutcome = "unchanged"
	// UpdateGroupLanguageOutcomeUpdated means the group language was updated.
	UpdateGroupLanguageOutcomeUpdated UpdateGroupLanguageOutcome = "updated"
)

type groupLanguageUpdateStore interface {
	OwnedCreatorForUser(ctx context.Context, ownerTelegramID int64) (core.Creator, bool, error)
	ManagedGroupByChatID(ctx context.Context, chatID int64) (core.ManagedGroup, bool, error)
	UpsertManagedGroup(ctx context.Context, group core.ManagedGroup) error
}

// UpdateGroupLanguageResult is the application-layer result for group-language updates.
type UpdateGroupLanguageResult struct {
	Outcome UpdateGroupLanguageOutcome
	Creator core.Creator
	Group   core.ManagedGroup
}

// GroupLanguageUpdateUseCase coordinates managed-group language updates.
type GroupLanguageUpdateUseCase struct {
	store  groupLanguageUpdateStore
	events events.EventSink
	now    func() time.Time
}

// NewGroupLanguageUpdateUseCase builds a group-language update use case.
func NewGroupLanguageUpdateUseCase(store groupLanguageUpdateStore, sink events.EventSink) *GroupLanguageUpdateUseCase {
	return &GroupLanguageUpdateUseCase{
		store:  store,
		events: events.EnsureSink(sink),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// UpdateGroupLanguage updates the language for a managed group owned by the caller.
func (u *GroupLanguageUpdateUseCase) UpdateGroupLanguage(ctx context.Context, ownerTelegramID, groupChatID int64, language string) (UpdateGroupLanguageResult, error) {
	group, managed, err := u.store.ManagedGroupByChatID(ctx, groupChatID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupLanguageResult{}, fmt.Errorf("load managed group by chat id: %w", err)
	}
	if !managed {
		u.recordOutcome(ctx, string(UpdateGroupLanguageOutcomeNotManaged))
		return UpdateGroupLanguageResult{Outcome: UpdateGroupLanguageOutcomeNotManaged}, nil
	}

	creator, ok, err := u.store.OwnedCreatorForUser(ctx, ownerTelegramID)
	if err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupLanguageResult{}, fmt.Errorf("load owned creator: %w", err)
	}
	if !ok || group.CreatorID != creator.ID {
		u.recordOutcome(ctx, string(UpdateGroupLanguageOutcomeNotOwner))
		return UpdateGroupLanguageResult{
			Outcome: UpdateGroupLanguageOutcomeNotOwner,
			Group:   group,
		}, nil
	}

	language = i18n.NormalizeLanguage(language)
	if group.Language == "" {
		group.Language = i18n.DefaultLanguage
	}
	if group.Language == language {
		u.recordOutcome(ctx, string(UpdateGroupLanguageOutcomeUnchanged))
		return UpdateGroupLanguageResult{
			Outcome: UpdateGroupLanguageOutcomeUnchanged,
			Creator: creator,
			Group:   group,
		}, nil
	}

	group.Language = language
	group.UpdatedAt = u.now()
	if err := u.store.UpsertManagedGroup(ctx, group); err != nil {
		u.recordOutcome(ctx, "failed")
		return UpdateGroupLanguageResult{}, fmt.Errorf("upsert managed group: %w", err)
	}

	u.recordOutcome(ctx, string(UpdateGroupLanguageOutcomeUpdated))
	return UpdateGroupLanguageResult{
		Outcome: UpdateGroupLanguageOutcomeUpdated,
		Creator: creator,
		Group:   group,
	}, nil
}

func (u *GroupLanguageUpdateUseCase) recordOutcome(ctx context.Context, outcome string) {
	u.events.Emit(ctx, events.Event{
		Name:    events.NameGroupLanguageUpdate,
		Outcome: outcome,
	})
}
