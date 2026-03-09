package flows

import (
	"strconv"
	"strings"
)

type callbackDomain string

const (
	callbackDomainViewer  callbackDomain = "viewer"
	callbackDomainCreator callbackDomain = "creator"
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
	domain callbackDomain
	verb   callbackVerb
	origin resetOrigin
	scope  resetScope
	target string
	chatID int64
}

func (a callbackAction) String() string {
	parts := []string{string(a.domain), string(a.verb)}
	if a.origin != "" {
		parts = append(parts, string(a.origin))
	}
	if a.scope != "" {
		parts = append(parts, string(a.scope))
	}
	if a.target != "" {
		parts = append(parts, a.target)
	}
	if a.chatID != 0 {
		parts = append(parts, strconv.FormatInt(a.chatID, 10))
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
		switch action.verb {
		case callbackVerbRefresh, callbackVerbRegister, callbackVerbReconnect:
			if len(parts) != 2 {
				return callbackAction{}, false
			}
			return action, true
		case callbackVerbMenu:
			if len(parts) != 2 {
				return callbackAction{}, false
			}
			return action, true
		case callbackVerbOpen, callbackVerbBack:
			if len(parts) != 3 {
				return callbackAction{}, false
			}
			action.target = parts[2]
			if !action.validCreatorTarget() {
				return callbackAction{}, false
			}
			return action, true
		case callbackVerbPick, callbackVerbExecute:
			if len(parts) != 4 {
				return callbackAction{}, false
			}
			action.target = parts[2]
			if action.target != creatorCallbackTargetGroup {
				return callbackAction{}, false
			}
			chatID, err := strconv.ParseInt(parts[3], 10, 64)
			if err != nil || chatID == 0 {
				return callbackAction{}, false
			}
			action.chatID = chatID
			return action, true
		case callbackVerbCancel:
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
			if len(parts) != 4 {
				return callbackAction{}, false
			}
			action.origin = resetOrigin(parts[2])
			action.scope = resetScope(parts[3])
			if !action.origin.valid() || !action.scope.valid() {
				return callbackAction{}, false
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

const (
	creatorCallbackTargetGroups = "groups"
	creatorCallbackTargetGroup  = "group"
)

func (a callbackAction) validCreatorTarget() bool {
	return a.target == creatorCallbackTargetGroups
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

func creatorGroupPickCallback(chatID int64) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbPick, target: creatorCallbackTargetGroup, chatID: chatID}.String()
}

func creatorGroupBackCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbBack, target: creatorCallbackTargetGroups}.String()
}

func creatorMenuCallback() string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbMenu}.String()
}

func creatorGroupExecuteCallback(chatID int64) string {
	return callbackAction{domain: callbackDomainCreator, verb: callbackVerbExecute, target: creatorCallbackTargetGroup, chatID: chatID}.String()
}

func resetOpenCallback(origin resetOrigin) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbOpen, origin: origin}.String()
}

func resetPickCallback(origin resetOrigin, scope resetScope) string {
	return callbackAction{domain: callbackDomainReset, verb: callbackVerbPick, origin: origin, scope: scope}.String()
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
