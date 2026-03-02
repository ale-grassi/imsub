package flows

import "net/url"

func (c *Controller) oauthStartURL(state string) string {
	return c.cfg.PublicBaseURL + "/auth/start/" + url.PathEscape(state)
}
