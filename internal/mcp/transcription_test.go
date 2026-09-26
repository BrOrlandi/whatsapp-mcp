package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
	"github.com/BrOrlandi/whatsapp-mcp/internal/transcribe"
)

type fakeTranscriber struct {
	key       string
	kept      map[string]store.Transcript
	sent      []transcribe.Audio
	languages []string
	saveErr   error
}

func (f *fakeTranscriber) Status(context.Context) (transcribe.Status, error) {
	if f.key == "" {
		return transcribe.Status{}, nil
	}
	return transcribe.Status{Configured: true, Hint: transcribe.Hint(f.key)}, nil
}
func (f *fakeTranscriber) SaveKey(_ context.Context, key string) (transcribe.Status, error) {
	if f.saveErr != nil {
		return transcribe.Status{}, f.saveErr
	}
	f.key = key
	return f.Status(context.Background())
}
func (f *fakeTranscriber) RemoveKey(context.Context) error {
	f.key = ""
	return nil
}
func (f *fakeTranscriber) Stored(_ context.Context, _ string, id string) (store.Transcript, error) {
	if t, ok := f.kept[id]; ok {
		return t, nil
	}
	return store.Transcript{}, store.ErrNoTranscript
}
func (f *fakeTranscriber) Transcribe(_ context.Context, instance, id string, audio transcribe.Audio, language string) (store.Transcript, error) {
	if f.key == "" {
		return store.Transcript{}, transcribe.ErrNotConfigured
	}
	f.sent = append(f.sent, audio)
	f.languages = append(f.languages, language)
	t := store.Transcript{InstanceID: instance, MessageID: id, Text: "oi, tudo bem?", Model: transcribe.Model}
	if f.kept == nil {
		f.kept = map[string]store.Transcript{}
	}
	f.kept[id] = t
	return t, nil
}

func audioIndex() *fakeIndex {
	return &fakeIndex{
		messages: []store.Message{
			{MessageID: "A1", ChatJID: "5511@s.whatsapp.net", MediaType: "audio", SenderName: "Ana"},
			{MessageID: "T1", ChatJID: "5511@s.whatsapp.net", Text: "olá"},
		},
		raw: []byte(`{"event":"Message","data":{"Message":{"audioMessage":{"seconds":3}}}}`),
	}
}

// A voice note is decoded through Evolution, sent once, and answered from the
// kept transcript afterwards, because every trip to Whisper is billed.
func TestTranscribeAudioSendsOnceAndAnswersFromTheKeptTranscript(t *testing.T) {
	transcriber := &fakeTranscriber{key: "sk-test-0000000000000000abcd"}
	live := &fakeLive{}
	server := testServer(audioIndex(), live, nil).WithTranscriber(transcriber, "https://mcp.example")

	payload, isError := call(t, server, "transcribe_audio", map[string]any{"message_id": "A1", "language": "pt"})
	if isError {
		t.Fatalf("transcription failed: %#v", payload)
	}
	if payload["transcript"].(map[string]any)["text"] != "oi, tudo bem?" || payload["cached"] != false {
		t.Fatalf("payload = %#v", payload)
	}
	if payload["content_warning"] == nil {
		t.Fatal("a transcript is third-party content and must carry the warning")
	}
	if len(transcriber.sent) != 1 || transcriber.sent[0].MimeType != "audio/ogg" || len(transcriber.sent[0].Data) == 0 || transcriber.languages[0] != "pt" {
		t.Fatalf("sent = %#v", transcriber.sent)
	}
	if live.tokensUsed[len(live.tokensUsed)-1] != "tok-1" {
		t.Fatalf("media was downloaded with %v", live.tokensUsed)
	}

	payload, _ = call(t, server, "transcribe_audio", map[string]any{"message_id": "A1"})
	if payload["cached"] != true || len(transcriber.sent) != 1 {
		t.Fatalf("second call paid again: %#v", payload)
	}
	call(t, server, "transcribe_audio", map[string]any{"message_id": "A1", "refresh": true})
	if len(transcriber.sent) != 2 {
		t.Fatal("refresh did not transcribe again")
	}
}

func TestTranscribeAudioRefusesWhatIsNotAVoiceNote(t *testing.T) {
	server := testServer(audioIndex(), &fakeLive{}, nil).WithTranscriber(&fakeTranscriber{key: "sk-x"}, "https://mcp.example")
	payload, isError := call(t, server, "transcribe_audio", map[string]any{"message_id": "T1"})
	if !isError || !strings.Contains(payload["error"].(string), "not a voice note") {
		t.Fatalf("payload = %#v", payload)
	}
	payload, isError = call(t, server, "transcribe_audio", map[string]any{"message_id": "nope"})
	if !isError || !strings.Contains(payload["error"].(string), "no indexed message") {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestTranscribeAudioWithoutAKeySaysHowToSaveOne(t *testing.T) {
	live := &fakeLive{}
	server := testServer(audioIndex(), live, nil).WithTranscriber(&fakeTranscriber{}, "https://mcp.example/")
	payload, isError := call(t, server, "transcribe_audio", map[string]any{"message_id": "A1"})
	if !isError || !strings.Contains(payload["error"].(string), "set_transcription_key") {
		t.Fatalf("payload = %#v", payload)
	}
	setup, ok := payload["setup"].(map[string]any)
	if !ok || setup["panel_url"] != "https://mcp.example/transcricao" {
		t.Fatalf("no setup directions: %#v", payload)
	}
	joined := ""
	for _, step := range setup["steps"].([]any) {
		joined += step.(string) + "\n"
	}
	for _, want := range []string{transcribe.SignupURL, transcribe.BillingURL, transcribe.KeysURL, "https://mcp.example/transcricao"} {
		if !strings.Contains(joined, want) {
			t.Errorf("setup steps do not mention %s", want)
		}
	}
	if len(live.tokensUsed) != 0 {
		t.Fatal("the audio was downloaded although there is no key to send it to")
	}
	status, _ := call(t, server, "whatsapp_status", nil)
	if status["transcription"].(map[string]any)["setup_url"] != "https://mcp.example/transcricao" {
		t.Fatalf("status = %#v", status["transcription"])
	}

	bare := testServer(audioIndex(), &fakeLive{}, nil)
	payload, isError = call(t, bare, "transcribe_audio", map[string]any{"message_id": "A1"})
	if !isError || !strings.Contains(payload["error"].(string), "not available") {
		t.Fatalf("payload = %#v", payload)
	}
}

// The key goes in and never comes back out: the result, and the status tool,
// only ever show its hint.
func TestSetTranscriptionKeyNeverEchoesTheKey(t *testing.T) {
	const key = "sk-proj-supersecretvalue-9876"
	transcriber := &fakeTranscriber{}
	server := testServer(audioIndex(), &fakeLive{}, nil).WithTranscriber(transcriber, "https://mcp.example")

	payload, isError := call(t, server, "set_transcription_key", map[string]any{"api_key": key})
	if isError || transcriber.key != key {
		t.Fatalf("payload = %#v", payload)
	}
	status, _ := call(t, server, "whatsapp_status", nil)
	for _, result := range []map[string]any{payload, status} {
		if strings.Contains(stringify(result), "supersecret") {
			t.Fatalf("the key leaked: %#v", result)
		}
	}
	if status["transcription"].(map[string]any)["configured"] != true {
		t.Fatalf("status = %#v", status)
	}

	transcriber.saveErr = transcribe.ErrInvalidKey
	payload, isError = call(t, server, "set_transcription_key", map[string]any{"api_key": "sk-refused-000000000000"})
	if !isError || !strings.Contains(payload["error"].(string), "refused") || payload["setup"] == nil {
		t.Fatalf("payload = %#v", payload)
	}

	payload, isError = call(t, server, "set_transcription_key", map[string]any{"remove": true})
	if isError || transcriber.key != "" {
		t.Fatalf("remove: %#v", payload)
	}
	if _, isError = call(t, server, "set_transcription_key", map[string]any{}); !isError {
		t.Fatal("an empty call must not pass as a save")
	}
}

func stringify(value map[string]any) string {
	var b strings.Builder
	for k, v := range value {
		b.WriteString(k)
		switch typed := v.(type) {
		case map[string]any:
			b.WriteString(stringify(typed))
		case string:
			b.WriteString(typed)
		}
	}
	return b.String()
}
