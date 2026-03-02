package handlers

import (
	"bytes"
	"html/template"
	"net/http"

	_ "embed"
)

//go:embed res/oauth_launch.html
var oauthLaunchHTML string

var oauthLaunchTmpl = template.Must(template.New("oauth_launch").Parse(oauthLaunchHTML))

func (c *Controller) renderOAuthLaunchPage(w http.ResponseWriter, oauthURL string) {
	var out bytes.Buffer
	if err := oauthLaunchTmpl.Execute(&out, map[string]string{
		"OAuthURL": oauthURL,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// A write error here only means the client connection closed early.
	_, _ = w.Write(out.Bytes())
}
