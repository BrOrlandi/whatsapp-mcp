package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/BrOrlandi/whatsapp-mcp/internal/transcribe"
)

// TranscriptionSettings is the part of the transcription service the panel
// drives: the OpenAI key, never the transcripts themselves, which are read
// from an AI client through transcribe_audio.
type TranscriptionSettings interface {
	Status(context.Context) (transcribe.Status, error)
	SaveKey(context.Context, string) (transcribe.Status, error)
	RemoveKey(context.Context) error
}

type transcriptionPage struct {
	layout
	Available      bool
	Status         transcribe.Status
	Saved          string
	Links          openAILinks
	PricePerMinute string
}

// openAILinks are the OpenAI pages the operator is sent to, before a key
// exists and after.
type openAILinks struct {
	Signup, Billing, Keys, Usage, Limits, Pricing string
}

var openAI = openAILinks{
	Signup:  transcribe.SignupURL,
	Billing: transcribe.BillingURL,
	Keys:    transcribe.KeysURL,
	Usage:   transcribe.UsageURL,
	Limits:  transcribe.LimitsURL,
	Pricing: transcribe.PricingURL,
}

func (a *webApp) transcriptionPage(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	page := transcriptionPage{layout: a.newLayout(r, "Transcrição de áudios", "transcricao"), Available: a.transcription != nil, Saved: r.URL.Query().Get("ok"), Links: openAI, PricePerMinute: strings.Replace(strconv.FormatFloat(transcribe.PricePerMinute, 'f', -1, 64), ".", ",", 1)}
	if a.transcription != nil {
		status, err := a.transcription.Status(r.Context())
		if err != nil {
			page.Error = "Não foi possível ler a configuração de transcrição."
		}
		page.Status = status
	}
	a.render(w, "transcricao", page)
}

func (a *webApp) saveTranscriptionKey(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	if a.transcription == nil {
		a.fail(w, r, "/transcricao", "A transcrição não está disponível neste servidor.")
		return
	}
	if _, err := a.transcription.SaveKey(r.Context(), r.FormValue("api_key")); err != nil {
		a.fail(w, r, "/transcricao", keyFailure(err))
		return
	}
	http.Redirect(w, r, "/transcricao?ok="+url.QueryEscape("Chave salva. Os áudios já podem ser transcritos."), http.StatusSeeOther)
}

func (a *webApp) removeTranscriptionKey(w http.ResponseWriter, r *http.Request) {
	if !a.require(w, r) {
		return
	}
	if a.transcription == nil {
		a.fail(w, r, "/transcricao", "A transcrição não está disponível neste servidor.")
		return
	}
	if err := a.transcription.RemoveKey(r.Context()); err != nil {
		a.fail(w, r, "/transcricao", "Não foi possível remover a chave.")
		return
	}
	http.Redirect(w, r, "/transcricao?ok="+url.QueryEscape("Chave removida. As transcrições já feitas continuam guardadas."), http.StatusSeeOther)
}

// keyFailure says why a key was refused in words the operator can act on.
// OpenAI's own text is logged rather than shown: it can quote the key back.
func keyFailure(err error) string {
	switch {
	case errors.Is(err, transcribe.ErrMalformedKey):
		return "Isso não parece uma chave da OpenAI. Ela começa com sk- e não tem espaços."
	case errors.Is(err, transcribe.ErrInvalidKey):
		return "A OpenAI recusou esta chave. Confira se ela foi copiada inteira e se ainda está ativa."
	case errors.Is(err, transcribe.ErrQuota):
		return "A conta da OpenAI desta chave está sem créditos. Adicione créditos em Billing, no painel da OpenAI, e tente de novo."
	default:
		slog.Warn("could not save the transcription key", "error", err)
		return "Não foi possível confirmar a chave com a OpenAI agora. Tente de novo em instantes."
	}
}
