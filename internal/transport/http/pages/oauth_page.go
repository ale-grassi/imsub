package pages

import (
	"bytes"
	"html/template"
	"net/http"
	"regexp"

	_ "embed"
)

//go:embed res/oauth_page.html
var oauthPageHTML string
var oauthPageTmpl = template.Must(template.New("oauth_page").Parse(oauthPageHTML))

type oauthTone string

const (
	oauthToneLaunch  oauthTone = "launch"
	oauthToneSuccess oauthTone = "success"
	oauthToneProblem oauthTone = "problem"
)

type oauthIcon string

const (
	oauthIconLaunch  oauthIcon = "launch"
	oauthIconSuccess oauthIcon = "success"
	oauthIconProblem oauthIcon = "problem"
)

type oauthTemplateData struct {
	DocumentTitle string
	Lang          string
	Tone          oauthTone
	Icon          oauthIcon
	IsProblem     bool
	Title         string
	MessageParts  []oauthTextPart
	Username      string
	PrimaryLabel  string
	PrimaryURL    string
	CopyLabel     string
	CopyIdleParts []oauthTextPart
	CopyDone      string
	NextStepParts []oauthTextPart
	RefreshURL    string
}

type oauthTextPart struct {
	Text      string
	IsCommand bool
}

// OAuthLaunchPage contains the user-facing content for the OAuth launch response.
type OAuthLaunchPage struct {
	Lang        string
	Title       string
	Message     string
	OAuthURL    string
	ButtonLabel string
	CopyLabel   string
	CopyIdle    string
	CopyDone    string
}

// OAuthSuccessPage contains the user-facing content for an OAuth success response.
type OAuthSuccessPage struct {
	Lang     string
	Title    string
	Message  string
	Username string
	NextStep string
}

// OAuthErrorPage contains the user-facing content for an OAuth error response.
type OAuthErrorPage struct {
	Lang     string
	Status   int
	Title    string
	Message  string
	NextStep string
}

func renderOAuthPage(w http.ResponseWriter, status int, data oauthTemplateData) {
	var out bytes.Buffer
	if err := oauthPageTmpl.Execute(&out, data); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(out.Bytes())
}

// RenderOAuthLaunch renders the OAuth launch HTML response.
func RenderOAuthLaunch(w http.ResponseWriter, page OAuthLaunchPage) {
	renderOAuthPage(w, http.StatusOK, oauthTemplateData{
		DocumentTitle: page.Title,
		Lang:          page.Lang,
		Tone:          oauthToneLaunch,
		Icon:          oauthIconLaunch,
		Title:         page.Title,
		MessageParts:  formatOAuthText(page.Message),
		PrimaryLabel:  page.ButtonLabel,
		PrimaryURL:    page.OAuthURL,
		CopyLabel:     page.CopyLabel,
		CopyIdleParts: formatOAuthText(page.CopyIdle),
		CopyDone:      page.CopyDone,
		RefreshURL:    page.OAuthURL,
	})
}

// RenderOAuthSuccess renders the OAuth success HTML response.
func RenderOAuthSuccess(w http.ResponseWriter, page OAuthSuccessPage) {
	renderOAuthPage(w, http.StatusOK, oauthTemplateData{
		DocumentTitle: page.Title,
		Lang:          page.Lang,
		Tone:          oauthToneSuccess,
		Icon:          oauthIconSuccess,
		Title:         page.Title,
		MessageParts:  formatOAuthText(page.Message),
		Username:      page.Username,
		NextStepParts: formatOAuthText(page.NextStep),
	})
}

// RenderOAuthError renders a user-facing OAuth error HTML response.
func RenderOAuthError(w http.ResponseWriter, page OAuthErrorPage) {
	renderOAuthPage(w, page.Status, oauthTemplateData{
		DocumentTitle: page.Title,
		Lang:          page.Lang,
		Tone:          oauthToneProblem,
		Icon:          oauthIconProblem,
		IsProblem:     true,
		Title:         page.Title,
		MessageParts:  formatOAuthText(page.Message),
		NextStepParts: formatOAuthText(page.NextStep),
	})
}

var slashCommandRE = regexp.MustCompile(`/[a-z][a-z0-9_]*`)

func formatOAuthText(text string) []oauthTextPart {
	matches := slashCommandRE.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return []oauthTextPart{{Text: text}}
	}

	parts := make([]oauthTextPart, 0, len(matches)*2+1)
	last := 0
	for _, match := range matches {
		if match[0] > last {
			parts = append(parts, oauthTextPart{Text: text[last:match[0]]})
		}
		parts = append(parts, oauthTextPart{Text: text[match[0]:match[1]], IsCommand: true})
		last = match[1]
	}
	if last < len(text) {
		parts = append(parts, oauthTextPart{Text: text[last:]})
	}

	return parts
}
