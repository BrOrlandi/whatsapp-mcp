package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
)

// MediaLinkTTL is how long a media link answers. Long enough to hand the URL
// to curl, short enough that a URL left in a transcript is dead by the time
// anyone reads it there.
const MediaLinkTTL = 10 * time.Minute

// mediaLinks are short-lived download URLs for the media of one message.
//
// download_media returns media as base64 inside the tool result, which puts a
// voice note into the model's context: expensive, and pointless when the
// client can run curl and would rather transcribe the file on its own machine.
// A link hands over the file instead. The token is the credential — random,
// bound to one message of one instance, and gone after MediaLinkTTL — so the
// client never has to dig the MCP bearer key out of its configuration to
// fetch it. Links live in memory: a restart invalidates them, and a client
// that needs the file again asks for a new one.
type mediaLinks struct {
	mu    sync.Mutex
	links map[string]mediaLink
	now   func() time.Time
}

type mediaLink struct {
	instanceID string
	messageID  string
	expires    time.Time
	// path is a file on the gateway's disk (a kept download, an export);
	// otherwise mimeType and data hold the file fetched when the link was
	// made, when it is small enough to keep, and it is fetched again on
	// download when it is not.
	path     string
	mimeType string
	data     []byte
}

// maxKeptMedia is the largest file a link keeps in memory. Voice notes and
// photos are far below it; a long video is fetched again when downloaded.
const maxKeptMedia = 25 << 20

func newMediaLinks() *mediaLinks {
	return &mediaLinks{links: map[string]mediaLink{}, now: time.Now}
}

func (l *mediaLinks) create(instanceID, messageID, mimeType string, data []byte) (string, time.Time, error) {
	link := mediaLink{instanceID: instanceID, messageID: messageID}
	if len(data) <= maxKeptMedia {
		link.mimeType, link.data = mimeType, data
	}
	return l.add(link)
}

// createFile hands out a link to a file on the gateway's disk.
func (l *mediaLinks) createFile(instanceID, path, mimeType string) (string, time.Time, error) {
	return l.add(mediaLink{instanceID: instanceID, path: path, mimeType: mimeType})
}

func (l *mediaLinks) add(link mediaLink) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for key, old := range l.links {
		if now.After(old.expires) {
			delete(l.links, key)
		}
	}
	link.expires = now.Add(MediaLinkTTL)
	l.links[token] = link
	return token, link.expires, nil
}

func (l *mediaLinks) resolve(token string) (mediaLink, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	link, ok := l.links[token]
	if !ok {
		return mediaLink{}, false
	}
	if l.now().After(link.expires) {
		delete(l.links, token)
		return mediaLink{}, false
	}
	return link, true
}

// MediaHandler serves GET /media/{token}. It is mounted outside /mcp because
// the token, not a bearer key, is what authorises it.
func (s *Server) MediaHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		link, ok := s.links.resolve(strings.TrimPrefix(r.URL.Path, "/media/"))
		if !ok {
			http.Error(w, "this media link does not exist or has expired; ask download_media for a new one", http.StatusNotFound)
			return
		}
		if link.path != "" {
			if link.mimeType != "" {
				w.Header().Set("Content-Type", link.mimeType)
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(link.path)))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			file, err := os.Open(link.path)
			if err != nil {
				http.Error(w, "this file is no longer on the gateway; ask for a new link", http.StatusGone)
				return
			}
			defer file.Close()
			info, err := file.Stat()
			if err != nil {
				http.Error(w, "this file cannot be read", http.StatusInternalServerError)
				return
			}
			http.ServeContent(w, r, "", info.ModTime(), file)
			return
		}
		mimeType, data := link.mimeType, link.data
		if data == nil {
			var status int
			var reason string
			mimeType, data, status, reason = s.fetchForLink(r, link)
			if data == nil {
				http.Error(w, reason, status)
				return
			}
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", link.messageID+extension(mimeType)))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	})
}

// fetchForLink downloads a link's media again, for files too large to have
// been kept. Media WhatsApp has discarded is a 410 with the reason, so it is
// not mistaken for this server being down.
func (s *Server) fetchForLink(r *http.Request, link mediaLink) (string, []byte, int, string) {
	session, err := s.resolve(r.Context(), link.instanceID)
	if err != nil {
		return "", nil, http.StatusGone, "the instance behind this link is no longer managed here"
	}
	payload, err := s.index.RawMessage(r.Context(), session.InstanceID, link.messageID)
	if err != nil {
		return "", nil, http.StatusNotFound, "this message is no longer in the index"
	}
	content := events.MessageContent(payload, link.messageID)
	if len(content) == 0 {
		return "", nil, http.StatusNotFound, "this message carries no media"
	}
	media, err := s.live.DownloadMedia(r.Context(), session.Token, content)
	switch {
	case errors.Is(err, evolution.ErrMediaExpired):
		return "", nil, http.StatusGone, "media expired on WhatsApp: its servers keep files only for a limited time, and only the sender resending it brings it back"
	case err != nil:
		return "", nil, http.StatusBadGateway, "could not fetch this media from WhatsApp: " + err.Error()
	}
	mimeType, data, err := decodeMedia(media, "application/octet-stream")
	if err != nil {
		return "", nil, http.StatusBadGateway, "WhatsApp returned no usable media"
	}
	return mimeType, data, 0, ""
}

// decodeMedia turns what Evolution's download route returns into bytes and a
// format. Evolution Go answers with a data URI in the base64 field —
// "data:audio/ogg; codecs=opus;base64,T2dnUw…" — and leaves mimetype empty, so
// the format has to be read from the URI's own header. A bare base64 body is
// accepted too; fallback names the format when neither does.
func decodeMedia(media evolution.Media, fallback string) (string, []byte, error) {
	body := strings.TrimSpace(media.Base64)
	mimeType := strings.TrimSpace(media.MimeType)
	if rest, ok := strings.CutPrefix(body, "data:"); ok {
		header, data, found := strings.Cut(rest, ",")
		if !found {
			return "", nil, errors.New("the data URI has no payload")
		}
		body = data
		if mimeType == "" {
			mimeType = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(header), ";base64"))
		}
	}
	if mimeType == "" {
		mimeType = fallback
	}
	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(body)
	}
	if err != nil {
		return "", nil, errors.New("the media is not valid base64")
	}
	if len(data) == 0 {
		return "", nil, errors.New("the media is empty")
	}
	return mimeType, data, nil
}

// extension names a downloaded file after its format, so a tool that decides
// by extension — ffmpeg, Whisper — reads it without being told.
func extension(mimeType string) string {
	base, _, err := mime.ParseMediaType(mimeType)
	if err != nil {
		return ""
	}
	switch base {
	case "audio/ogg", "audio/opus":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "application/pdf":
		return ".pdf"
	}
	if extensions, err := mime.ExtensionsByType(base); err == nil && len(extensions) > 0 {
		return extensions[0]
	}
	return ""
}

// mimeOfExt names a file's format from its extension, for a kept file whose
// message did not say.
func mimeOfExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".pdf":
		return "application/pdf"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}
