package flows

import (
	"context"
	"fmt"
	"html"

	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/usecase"

	"github.com/mymmrac/telego"
)

type groupRegistrationView struct {
	text             string
	opts             client.MessageOptions
	groupBaseText    string
	dispatchFollowUp bool
}

func buildGroupRegistrationView(lang string, replyToMessageID int, regRes usecase.RegisterGroupResult) (groupRegistrationView, bool) {
	view := groupRegistrationView{
		opts: client.MessageOptions{ReplyToMessageID: replyToMessageID},
	}

	switch regRes.Outcome {
	case usecase.RegisterGroupOutcomeNotCreator:
		view.text = i18n.Translate(lang, msgGroupNotCreator)
	case usecase.RegisterGroupOutcomeTakenByOther:
		view.text = fmt.Sprintf(i18n.Translate(lang, msgGroupTakenByOther), html.EscapeString(regRes.OtherCreatorName))
		view.opts.ParseMode = telego.ModeHTML
	case usecase.RegisterGroupOutcomeAlreadyLinked:
		view.groupBaseText = fmt.Sprintf(i18n.Translate(lang, msgGroupAlreadyLinked), html.EscapeString(regRes.Creator.Name))
		view.text = joinNonEmptySections(
			textSection{text: view.groupBaseText},
			textSection{text: i18n.Translate(lang, msgGroupCheckingSettings)},
		)
		view.opts.ParseMode = telego.ModeHTML
		view.dispatchFollowUp = true
	case usecase.RegisterGroupOutcomeRegistered:
		view.groupBaseText = fmt.Sprintf(i18n.Translate(lang, msgGroupRegistered), html.EscapeString(regRes.Creator.Name))
		view.text = joinNonEmptySections(
			textSection{text: view.groupBaseText},
			textSection{text: i18n.Translate(lang, msgGroupCheckingSettings)},
		)
		view.opts.ParseMode = telego.ModeHTML
		view.dispatchFollowUp = true
	default:
		return groupRegistrationView{}, false
	}

	return view, true
}

func (c *Controller) dispatchGroupRegistrationFollowUp(ctx context.Context, msg telego.Message, lang string, regRes usecase.RegisterGroupResult, view groupRegistrationView, groupMsgID int) {
	if !regRes.FollowUp.NeedsActivation && !regRes.FollowUp.NeedsSettingsCheck {
		return
	}

	if regRes.FollowUp.NeedsActivation {
		// Activation runs asynchronously to keep the command response fast.
		// The goroutine terminates when either:
		//  - all operations complete, or
		//  - the 3-minute timeout in activateCreatorOnFirstGroupRegistration fires.
		// context.WithoutCancel is used so the work survives the parent
		// request context being canceled.
		c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
			c.activateCreatorOnFirstGroupRegistration(bg, regRes.Creator, msg.Chat.ID, lang)
		})
	}

	if !regRes.FollowUp.NeedsSettingsCheck {
		return
	}
	if regRes.FollowUp.NotifyOwner {
		c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
			c.sendPostRegistrationMessages(bg, postRegistrationMessageOptions{
				groupChatID:   msg.Chat.ID,
				groupMsgID:    groupMsgID,
				ownerUserID:   msg.From.ID,
				groupName:     msg.Chat.Title,
				creatorName:   regRes.Creator.Name,
				lang:          lang,
				groupBaseText: view.groupBaseText,
			})
		})
		return
	}

	c.runBackground(context.WithoutCancel(ctx), func(bg context.Context) {
		c.sendPostRegistrationSettingsCheck(bg, msg.Chat.ID, groupMsgID, lang, view.groupBaseText)
	})
}
