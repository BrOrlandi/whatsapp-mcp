package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
	"github.com/BrOrlandi/whatsapp-mcp/internal/transcribe"
)

// inlineLimit is the largest file download_media puts inside its result;
// anything larger comes as a link.
const inlineLimit = 20 << 20

func (s *Server) downloadMedia(ctx context.Context, session Session, args arguments) map[string]any {
	m, failure := s.targetIn(ctx, session, args.MessageID, args.ChatJID)
	if failure != nil {
		return failure
	}
	d, failure := s.mediaFile(ctx, session, m)
	if failure != nil {
		return failure
	}
	result := map[string]any{"message_id": m.MessageID, "chat_jid": m.ChatJID, "file": filepath.Base(d.path), "bytes": d.bytes,
		"media_type": m.MediaType, "mime_type": d.mimeType, "content_warning": UntrustedContent}
	if m.Filename != "" {
		result["filename"] = m.Filename
	}
	if d.cached {
		result["kept"] = "served from the copy the gateway kept when it was first downloaded"
	}
	link := func(note string) map[string]any {
		if s.publicURL == "" {
			return toolError("this gateway has no public URL configured, so it cannot hand out download links; call download_media without link")
		}
		token, expires, err := s.links.createFile(session.InstanceID, d.path, d.mimeType)
		if err != nil {
			return toolError("could not create a download link: %v", err)
		}
		url := s.publicURL + "/media/" + token
		result["url"], result["expires_at"] = url, expires.UTC()
		result["curl"] = fmt.Sprintf("curl -fsSL -o %q '%s'", filepath.Base(d.path), url)
		result["note"] = note
		return textResult(result, false)
	}
	if args.Link {
		return link("The URL needs no credential and stops working at expires_at. Treat it as a secret until then. It can also be passed to send_media_message to send this file again.")
	}
	if d.bytes > inlineLimit {
		return link("The file is larger than 20 MiB, so it is not inlined: download it from url, which needs no credential and stops working at expires_at.")
	}
	body, err := os.ReadFile(d.path)
	if err != nil {
		return toolError("%v", err)
	}
	meta, _ := json.MarshalIndent(result, "", "  ")
	content := []any{map[string]any{"type": "text", "text": string(meta)}}
	encoded := base64.StdEncoding.EncodeToString(body)
	mimeType := strings.TrimSpace(strings.Split(d.mimeType, ";")[0])
	switch {
	case m.MediaType == "image" || m.MediaType == "sticker":
		content = append(content, map[string]any{"type": "image", "data": encoded, "mimeType": orDefault(mimeType, "image/jpeg")})
	case m.MediaType == "audio":
		content = append(content, map[string]any{"type": "audio", "data": encoded, "mimeType": orDefault(mimeType, "audio/ogg")})
	default:
		content = append(content, map[string]any{"type": "resource", "resource": map[string]any{
			"uri": "whatsapp-media://" + session.InstanceID + "/" + m.MessageID, "mimeType": orDefault(mimeType, "application/octet-stream"), "blob": encoded}})
	}
	return map[string]any{"content": content, "isError": false}
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// locationOf reads the point a location message carries from its stored
// event.
func locationOf(ctx context.Context, s *Server, session Session, m store.Message) *events.Location {
	payload, err := s.index.RawMessage(ctx, session.InstanceID, m.MessageID)
	if err != nil {
		return nil
	}
	return events.ReadDetails(events.MessageContent(payload, m.MessageID)).Location
}

// reviewGuidance asks the client to do what speech recognition cannot: read
// the transcript against the conversation and fix what was misheard.
const reviewGuidance = "This transcript was made by speech recognition, which mishears names, places and jargon. Read it against the context messages: where a word is clearly a mishearing of something the conversation mentions (a name, a place, a product, a term), call save_transcript with the corrected text. Change only what the context makes certain; keep the speaker's wording, slang and mistakes otherwise, and never add content. The original stays kept as raw_text."

// audioContext is the conversation around a voice note: who is in it and
// what was said just before and after, for the engine and for the review.
type audioContext struct {
	Chat     string        `json:"chat,omitempty"`
	People   []string      `json:"people,omitempty"`
	Messages []contextLine `json:"messages,omitempty"`
}

type contextLine struct {
	Sender string `json:"sender,omitempty"`
	Text   string `json:"text"`
}

func (s *Server) audioContext(ctx context.Context, session Session, m store.Message) audioContext {
	earlier, later, err := s.index.MessageContext(ctx, session.InstanceID, m, 12, 4)
	if err != nil {
		return audioContext{}
	}
	names := s.namer(session)
	out := audioContext{Chat: names.name(ctx, m.ChatJID)}
	seen := map[string]bool{}
	for _, row := range append(earlier, later...) {
		sender := row.SenderName
		if row.FromMe {
			sender = "me"
		} else if sender == "" {
			sender = names.name(ctx, row.SenderJID)
		}
		if sender != "" && sender != "me" && !seen[sender] {
			seen[sender] = true
			out.People = append(out.People, sender)
		}
		text := row.Text
		if text == "" && row.Transcript != "" {
			text = "[transcript] " + row.Transcript
		}
		if text = strings.TrimSpace(text); text != "" {
			out.Messages = append(out.Messages, contextLine{Sender: sender, Text: clip(text, 300)})
		}
	}
	return out
}

// prompt is the context as Whisper takes it: a short text in the
// conversation's words, which steers how names and terms are spelled.
func (c audioContext) prompt() string {
	var b strings.Builder
	if len(c.People) > 0 || c.Chat != "" {
		b.WriteString(strings.Join(append([]string{c.Chat}, c.People...), ", "))
		b.WriteString(". ")
	}
	for _, line := range c.Messages {
		b.WriteString(line.Text)
		b.WriteString(" ")
	}
	return clip(strings.TrimSpace(b.String()), 800)
}

// transcribeAudio turns a voice note into text.
//
// A kept transcript is answered first, because the audio never changes and
// every trip to Whisper is billed to the operator. The transcript is WhatsApp
// content like any other — a third party spoke it — so it travels with the
// same warning as the messages themselves.
func (s *Server) transcribeAudio(ctx context.Context, session Session, args arguments) map[string]any {
	if s.transcriber == nil {
		return toolError("transcription is not available on this gateway")
	}
	message, failure := s.targetIn(ctx, session, args.MessageID, args.ChatJID)
	if failure != nil {
		return failure
	}
	if message.MediaType != "audio" {
		kind := message.MediaType
		if kind == "" || kind == "text" {
			kind = "a text message"
		}
		return toolError("message %q is %s, not a voice note; only messages whose media_type is audio can be transcribed", args.MessageID, kind)
	}
	audioCtx := s.audioContext(ctx, session, message)
	if !args.Refresh {
		if kept, err := s.transcriber.Stored(ctx, session.InstanceID, message.MessageID); err == nil {
			return s.readResult(ctx, session, map[string]any{"transcript": kept, "message": describe(message), "cached": true, "context": audioCtx, "review": reviewGuidance})
		}
	}
	// Checked before the audio is downloaded: without a key there is nothing
	// to send it to, and the user needs directions rather than a download.
	if status, err := s.transcriber.Status(ctx); err == nil && !status.Configured {
		return s.transcriptionError(transcribe.ErrNotConfigured)
	}
	d, failure := s.mediaFile(ctx, session, message)
	if failure != nil {
		return failure
	}
	data, err := os.ReadFile(d.path)
	if err != nil {
		return toolError("%v", err)
	}
	audio := transcribe.Audio{MimeType: orDefault(d.mimeType, "audio/ogg"), Data: data, Prompt: audioCtx.prompt()}
	transcript, err := s.transcriber.Transcribe(ctx, session.InstanceID, message.MessageID, audio, args.Language)
	if err != nil {
		return s.transcriptionError(err)
	}
	return s.readResult(ctx, session, map[string]any{"transcript": transcript, "message": describe(message), "cached": false, "context": audioCtx, "review": reviewGuidance})
}

// saveTranscript stores a transcript made outside the gateway, or a
// correction of one. It needs no OpenAI key: the point is that the text was
// produced somewhere else.
func (s *Server) saveTranscript(ctx context.Context, session Session, args arguments) map[string]any {
	if s.transcriber == nil {
		return toolError("transcription is not available on this gateway")
	}
	text := strings.TrimSpace(args.Text)
	if text == "" {
		return toolError("text is required")
	}
	if len(text) > maxTranscript {
		return toolError("text is longer than %d characters, which is more than any voice note holds", maxTranscript)
	}
	message, failure := s.targetIn(ctx, session, args.MessageID, args.ChatJID)
	if failure != nil {
		return failure
	}
	if message.MediaType != "audio" {
		return toolError("message %q is not a voice note; transcripts are kept only for messages whose media_type is audio", args.MessageID)
	}
	model := strings.TrimSpace(args.Model)
	transcript := store.Transcript{InstanceID: session.InstanceID, MessageID: message.MessageID, Text: text, Language: strings.TrimSpace(args.Language), Model: model}
	// A correction keeps what was first heard, so it can be checked.
	corrected := false
	if prev, err := s.transcriber.Stored(ctx, session.InstanceID, message.MessageID); err == nil {
		corrected = true
		transcript.Raw = prev.Raw
		if transcript.Raw == "" {
			transcript.Raw = prev.Text
		}
		if transcript.Model == "" {
			transcript.Model = prev.Model
		}
		if transcript.Language == "" {
			transcript.Language = prev.Language
		}
		transcript.Duration = prev.Duration
	}
	if transcript.Model == "" {
		transcript.Model = "external"
	}
	if err := s.transcriber.Keep(ctx, transcript); err != nil {
		return toolError("could not store the transcript: %v", err)
	}
	return textResult(map[string]any{"saved": true, "corrected": corrected, "message": describe(message), "transcript": transcript,
		"note": "The reading tools now return this text in the message's transcript field, and search matches it."}, false)
}

// maxTranscript bounds a stored transcript. An hour of speech is around
// sixty thousand characters; anything far past that is not a transcript.
const maxTranscript = 200000
