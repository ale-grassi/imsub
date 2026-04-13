package core

import "maps"

// GodAccessChecker reports whether a Telegram user should bypass normal access
// checks and notifications globally.
type GodAccessChecker struct {
	ids map[int64]struct{}
}

// NewGodAccessChecker builds a checker from the provided Telegram user IDs.
func NewGodAccessChecker(ids ...int64) *GodAccessChecker {
	set := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		set[id] = struct{}{}
	}
	return &GodAccessChecker{ids: set}
}

// WithIDs returns a checker containing the existing IDs plus the provided ones.
func (c *GodAccessChecker) WithIDs(ids ...int64) *GodAccessChecker {
	if c == nil {
		return NewGodAccessChecker(ids...)
	}
	out := &GodAccessChecker{ids: maps.Clone(c.ids)}
	if out.ids == nil {
		out.ids = make(map[int64]struct{}, len(ids))
	}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		out.ids[id] = struct{}{}
	}
	return out
}

// IsGodTelegramUser reports whether telegramUserID is globally bypassed.
func (c *GodAccessChecker) IsGodTelegramUser(telegramUserID int64) bool {
	if c == nil || telegramUserID == 0 {
		return false
	}
	_, ok := c.ids[telegramUserID]
	return ok
}
