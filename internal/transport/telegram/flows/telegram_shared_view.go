package flows

import (
	"fmt"

	"imsub/internal/platform/i18n"
	"imsub/internal/transport/telegram/client"
	"imsub/internal/transport/telegram/ui"

	"github.com/mymmrac/telego"
)

type sharedView struct {
	text string
	opts client.MessageOptions
}

func buildTextView(lang, key string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, key),
	}
}

func buildMainMenuTextView(lang, key string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, key),
		opts: client.MessageOptions{
			Markup: viewerMainMenuMarkup(lang),
		},
	}
}

func buildHTMLTextView(lang, key string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, key),
		opts: client.MessageOptions{
			ParseMode: telego.ModeHTML,
		},
	}
}

func buildViewerStatusErrorView(lang string) sharedView {
	return buildMainMenuTextView(lang, msgErrLoadStatus)
}

func buildCreatorStatusErrorView(lang string) sharedView {
	return buildTextView(lang, msgErrLoadStatus)
}

func buildCreatorLinkErrorView(lang string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgErrCreatorLink),
		opts: client.MessageOptions{
			Markup: creatorMainMenuMarkup(lang),
		},
	}
}

func buildCreatorReconnectRequiredView(lang, reconnectURL string) sharedView {
	return sharedView{
		text: i18n.Translate(lang, msgCreatorReconnectNeeded),
		opts: client.MessageOptions{
			ParseMode: telego.ModeHTML,
			Markup:    ui.CreatorStatusMenuMarkup(lang, reconnectURL, creatorStatusMenuCallbacks(false)),
		},
	}
}

func buildSubscriptionEndView(lang, viewerLogin, broadcasterLogin string) sharedView {
	return sharedView{
		text: fmt.Sprintf(i18n.Translate(lang, msgSubEndPartial), viewerLogin),
		opts: client.MessageOptions{
			ParseMode: telego.ModeHTML,
			Markup:    ui.SubEndSubscribeMarkup(lang, broadcasterLogin),
		},
	}
}
