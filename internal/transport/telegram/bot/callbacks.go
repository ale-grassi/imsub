package bot

import (
	"strconv"
	"strings"

	"imsub/internal/core"
)

type callbackDomain string

const (
	callbackDomainViewer  callbackDomain = "viewer"
	callbackDomainCreator callbackDomain = "creator"
	callbackDomainGroup   callbackDomain = "group"
	callbackDomainReset   callbackDomain = "reset"
)

type callbackVerb string

const (
	callbackVerbRefresh   callbackVerb = "refresh"
	callbackVerbRegister  callbackVerb = "register"
	callbackVerbReconnect callbackVerb = "reconnect"
	callbackVerbOpen      callbackVerb = "open"
	callbackVerbPick      callbackVerb = "pick"
	callbackVerbBack      callbackVerb = "back"
	callbackVerbMenu      callbackVerb = "menu"
	callbackVerbCancel    callbackVerb = "cancel"
	callbackVerbExecute   callbackVerb = "exec"
)

type resetOrigin string

const (
	resetOriginViewer  resetOrigin = "viewer"
	resetOriginCreator resetOrigin = "creator"
	resetOriginCommand resetOrigin = "command"
)

type resetScope string

const (
	resetScopeViewer  resetScope = "viewer"
	resetScopeCreator resetScope = "creator"
	resetScopeBoth    resetScope = "both"
)

type callbackAction struct {
	domain      callbackDomain
	verb        callbackVerb
	origin      resetOrigin
	scope       resetScope
	target      string
	policy      core.GroupPolicy
	grace       core.SubscriptionEndGrace
	resetAction core.CreatorResetGroupAction
	chatID      int64
	threadID    int
}

func (a callbackAction) String() string {
	parts := []string{string(a.domain), string(a.verb)}
	if a.origin != "" {
		parts = append(parts, string(a.origin))
	}
	if a.scope != "" {
		parts = append(parts, string(a.scope))
	}
	if a.resetAction != "" {
		parts = append(parts, string(a.resetAction))
	}
	if a.target != "" {
		parts = append(parts, a.target)
	}
	if a.policy != "" {
		parts = append(parts, string(a.policy))
	}
	if a.grace != "" {
		parts = append(parts, string(a.grace))
	}
	if a.chatID != 0 {
		parts = append(parts, strconv.FormatInt(a.chatID, 10))
	}
	if a.threadID != 0 {
		parts = append(parts, strconv.Itoa(a.threadID))
	}
	return strings.Join(parts, ":")
}

func parseCallbackAction(data string) (callbackAction, bool) {
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return callbackAction{}, false
	}

	action := callbackAction{
		domain: callbackDomain(parts[0]),
		verb:   callbackVerb(parts[1]),
	}
	switch action.domain {
	case callbackDomainViewer:
		if len(parts) != 2 || action.verb != callbackVerbRefresh {
			return callbackAction{}, false
		}
		return action, true
	case callbackDomainCreator:
		return parseCreatorCallbackAction(action, parts)
	case callbackDomainGroup:
		switch action.verb {
		case callbackVerbPick:
			if len(parts) != 4 && len(parts) != 5 {
				return callbackAction{}, false
			}
			action.policy = core.GroupPolicy(parts[2])
			if !validGroupPolicy(action.policy) {
				return callbackAction{}, false
			}
			chatID, err := strconv.ParseInt(parts[3], 10, 64)
			if err != nil || chatID == 0 {
				return callbackAction{}, false
			}
			action.chatID = chatID
			if len(parts) == 5 {
				threadID, err := strconv.Atoi(parts[4])
				if err != nil || threadID <= 0 {
					return callbackAction{}, false
				}
				action.threadID = threadID
			}
			return action, true
		case callbackVerbExecute:
			if len(parts) != 4 {
				return callbackAction{}, false
			}
			action.resetAction = core.CreatorResetGroupAction(parts[2])
			if !validResetAction(action.resetAction) {
				return callbackAction{}, false
			}
			chatID, err := strconv.ParseInt(parts[3], 10, 64)
			if err != nil || chatID == 0 {
				return callbackAction{}, false
			}
			action.chatID = chatID
			return action, true
		case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect, callbackVerbOpen, callbackVerbBack, callbackVerbMenu, callbackVerbCancel:
			return callbackAction{}, false
		default:
			return callbackAction{}, false
		}
	case callbackDomainReset:
		switch action.verb {
		case callbackVerbOpen, callbackVerbBack, callbackVerbMenu, callbackVerbCancel:
			if len(parts) != 3 {
				return callbackAction{}, false
			}
			action.origin = resetOrigin(parts[2])
			if !action.origin.valid() {
				return callbackAction{}, false
			}
			return action, true
		case callbackVerbPick, callbackVerbExecute:
			if len(parts) != 4 && len(parts) != 5 {
				return callbackAction{}, false
			}
			action.origin = resetOrigin(parts[2])
			action.scope = resetScope(parts[3])
			if !action.origin.valid() || !action.scope.valid() {
				return callbackAction{}, false
			}
			if len(parts) == 5 {
				action.resetAction = core.CreatorResetGroupAction(parts[4])
				if !validResetAction(action.resetAction) {
					return callbackAction{}, false
				}
			}
			return action, true
		case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect:
			return callbackAction{}, false
		default:
			return callbackAction{}, false
		}
	default:
		return callbackAction{}, false
	}
}

func parseCreatorCallbackAction(action callbackAction, parts []string) (callbackAction, bool) {
	switch action.verb {
	case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect, callbackVerbMenu:
		return action, len(parts) == 2
	case callbackVerbOpen, callbackVerbBack:
		return parseCreatorOpenBackAction(action, parts)
	case callbackVerbPick, callbackVerbExecute:
		return parseCreatorPickExecuteAction(action, parts)
	case callbackVerbCancel:
		return callbackAction{}, false
	default:
		return callbackAction{}, false
	}
}

func parseCreatorOpenBackAction(action callbackAction, parts []string) (callbackAction, bool) {
	switch len(parts) {
	case 3:
		action.target = parts[2]
		if !validCreatorOpenTarget(action.target) {
			return callbackAction{}, false
		}
		return action, true
	case 4:
		action.target = parts[2]
		if !validCreatorOpenChatTarget(action.target) {
			return callbackAction{}, false
		}
		chatID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || chatID == 0 {
			return callbackAction{}, false
		}
		action.chatID = chatID
		return action, true
	default:
		return callbackAction{}, false
	}
}

func parseCreatorPickExecuteAction(action callbackAction, parts []string) (callbackAction, bool) {
	switch len(parts) {
	case 3:
		action.target = parts[2]
		if action.verb != callbackVerbExecute || action.target != creatorCallbackTargetBlocklist {
			return callbackAction{}, false
		}
		return action, true
	case 4:
		action.target = parts[2]
		if action.target == creatorCallbackTargetGrace {
			if action.verb != callbackVerbExecute {
				return callbackAction{}, false
			}
			action.grace = core.SubscriptionEndGrace(parts[3])
			if !validSubscriptionEndGrace(action.grace) {
				return callbackAction{}, false
			}
			return action, true
		}
		if action.target != creatorCallbackTargetGroup {
			return callbackAction{}, false
		}
		chatID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || chatID == 0 {
			return callbackAction{}, false
		}
		action.chatID = chatID
		return action, true
	case 5:
		action.target = parts[2]
		switch action.target {
		case creatorCallbackTargetPolicy:
			action.policy = core.GroupPolicy(parts[3])
			if !validGroupPolicy(action.policy) {
				return callbackAction{}, false
			}
		case creatorCallbackTargetGroup:
			if action.verb != callbackVerbExecute {
				return callbackAction{}, false
			}
			action.resetAction = core.CreatorResetGroupAction(parts[3])
			if !validResetAction(action.resetAction) {
				return callbackAction{}, false
			}
		default:
			return callbackAction{}, false
		}
		chatID, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil || chatID == 0 {
			return callbackAction{}, false
		}
		action.chatID = chatID
		return action, true
	default:
		return callbackAction{}, false
	}
}

const (
	creatorCallbackTargetGroups       = "groups"
	creatorCallbackTargetGroup        = "group"
	creatorCallbackTargetGroupConfirm = "group_confirm"
	creatorCallbackTargetBlocklist    = "blocklist"
	creatorCallbackTargetGrace        = "grace"
	creatorCallbackTargetPolicy       = "policy"
)

func validCreatorOpenTarget(target string) bool {
	return target == creatorCallbackTargetGroups || target == creatorCallbackTargetBlocklist || target == creatorCallbackTargetGrace
}

func validCreatorOpenChatTarget(target string) bool {
	return target == creatorCallbackTargetGroupConfirm || target == creatorCallbackTargetPolicy
}

func (o resetOrigin) valid() bool {
	switch o {
	case resetOriginViewer, resetOriginCreator, resetOriginCommand:
		return true
	default:
		return false
	}
}

func (s resetScope) valid() bool {
	switch s {
	case resetScopeViewer, resetScopeCreator, resetScopeBoth:
		return true
	default:
		return false
	}
}

func validResetAction(a core.CreatorResetGroupAction) bool {
	switch a {
	case core.CreatorResetKeepMembers, core.CreatorResetKickTrackedMembers:
		return true
	default:
		return false
	}
}

func validGroupPolicy(p core.GroupPolicy) bool {
	switch p {
	case core.GroupPolicyObserve, core.GroupPolicyObserveWarn, core.GroupPolicyKick, core.GroupPolicyGraceWeek:
		return true
	default:
		return false
	}
}

func validSubscriptionEndGrace(g core.SubscriptionEndGrace) bool {
	switch g {
	case core.SubscriptionEndGraceOff, core.SubscriptionEndGrace24h, core.SubscriptionEndGrace48h, core.SubscriptionEndGrace72h:
		return true
	default:
		return false
	}
}

func viewerRefreshCallback() string {
	return callbackAction{domain: callbackDomainViewer, verb: callbackVerbRefresh}.String()
}

func creatorRefreshCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbRefresh}.String()
}

func creatorReconnectCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbReconnect}.String()
}

func creatorManageGroupsCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetGroups}.String()
}

func creatorGraceOpenCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetGrace}.String()
}

func creatorGroupPickCallback(chatID int64) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbPick, target: creatorCallbackTargetGroup, chatID: chatID}.String()
}

func creatorGroupPolicyOpenCallback(chatID int64) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetPolicy, chatID: chatID}.String()
}

func creatorGroupConfirmCallback(chatID int64) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbOpen, target: creatorCallbackTargetGroupConfirm, chatID: chatID}.String()
}

func creatorGroupPolicyPickCallback(chatID int64, policy core.GroupPolicy) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbPick, target: creatorCallbackTargetPolicy, policy: policy, chatID: chatID}.String()
}

func creatorGroupPolicyExecuteCallback(chatID int64, policy core.GroupPolicy) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbExecute, target: creatorCallbackTargetPolicy, policy: policy, chatID: chatID}.String()
}

func creatorGroupBackCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbBack, target: creatorCallbackTargetGroups}.String()
}

func creatorMenuCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbMenu}.String()
}

func creatorGroupExecuteWithActionCallback(chatID int64, action core.CreatorResetGroupAction) string {
	return strings.Join([]string{
		string(callbackDomainCreator),
		string(callbackVerbExecute),
		creatorCallbackTargetGroup,
		string(action),
		strconv.FormatInt(chatID, 10),
	}, ":")
}

func creatorBlocklistToggleCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbExecute, target: creatorCallbackTargetBlocklist}.String()
}

func creatorGraceExecuteCallback(grace core.SubscriptionEndGrace) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbExecute, target: creatorCallbackTargetGrace, grace: grace}.String()
}

func groupRegisterPolicyCallback(chatID int64, threadID int, policy core.GroupPolicy) string {
	return callbackAction{domain: callbackDomainGroup, verb: callbackVerbPick, policy: policy, chatID: chatID, threadID: threadID}.String()
}

func groupUnregisterExecuteCallback(chatID int64, action core.CreatorResetGroupAction) string {
	return callbackAction{domain: callbackDomainGroup, verb: callbackVerbExecute, resetAction: action, chatID: chatID}.String()
}

func resetOpenCallback(origin resetOrigin) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbOpen, origin: origin}.String()
}

func resetPickCallback(origin resetOrigin, scope resetScope) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbPick, origin: origin, scope: scope}.String()
}

func resetActionPickCallback(origin resetOrigin, scope resetScope, action core.CreatorResetGroupAction) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbPick, origin: origin, scope: scope, resetAction: action}.String()
}

func resetBackCallback(origin resetOrigin) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbBack, origin: origin}.String()
}

func resetMenuCallback(origin resetOrigin) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbMenu, origin: origin}.String()
}

func resetCancelCallback(origin resetOrigin) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbCancel, origin: origin}.String()
}

func resetExecuteCallback(origin resetOrigin, scope resetScope) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbExecute, origin: origin, scope: scope}.String()
}

func resetExecuteWithActionCallback(origin resetOrigin, scope resetScope, action core.CreatorResetGroupAction) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbExecute, origin: origin, scope: scope, resetAction: action}.String()
}
