package pages

import (
	"bytes"
	"html/template"
	"net/http"

	_ "embed"
)

//go:embed res/oauth_success.html
var oauthSuccessHTML string
var oauthSuccessTmpl = template.Must(template.New("oauth_success").Parse(oauthSuccessHTML))

//go:embed res/oauth_error.html
var oauthErrorHTML string
var oauthErrorTmpl = template.Must(template.New("oauth_error").Parse(oauthErrorHTML))

// OAuthErrorPage contains the user-facing content for an OAuth error response.
type OAuthErrorPage struct {
	Status  int
	Title   string
	Message string
	Hint    string
}

// RenderOAuthSuccess renders the OAuth success HTML response.
func RenderOAuthSuccess(w http.ResponseWriter, title, message, username string) {
	var out bytes.Buffer
	if err := oauthSuccessTmpl.Execute(&out, map[string]string{
		"Title":    title,
		"Message":  message,
		"Username": username,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	// A write error here only means the client connection closed early.
	_, _ = w.Write(out.Bytes())
}

// RenderOAuthError renders a user-facing OAuth error HTML response.
func RenderOAuthError(w http.ResponseWriter, page OAuthErrorPage) {
	var out bytes.Buffer
	if err := oauthErrorTmpl.Execute(&out, map[string]string{
		"Title":   page.Title,
		"Message": page.Message,
		"Hint":    page.Hint,
	}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(page.Status)
	_, _ = w.Write(out.Bytes())
}
