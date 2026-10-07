package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The media folder holds what the tools downloaded on request (download_media,
// transcribe_audio, forward_message), one folder per instance and chat:
// <media dir>/<instance>/<chat>/<message id><ext>. A file kept there is served
// from disk the next time it is asked for, which also keeps it readable after
// WhatsApp discards it from its servers. Every file was downloadable from
// WhatsApp when it was saved, so clearing the folder frees space without
// losing a message, only the copies.

// RetentionSetting is the setting key for how many days downloaded media is
// kept; empty or 0 keeps it forever.
const RetentionSetting = "media_retention_days"

type mediaFile struct {
	path    string
	chat    string
	kind    string
	size    int64
	modTime time.Time
}

func mediaKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".heic", ".bmp", ".tif", ".tiff":
		return "image"
	case ".mp4", ".mov", ".3gp", ".mkv", ".webm":
		return "video"
	case ".ogg", ".opus", ".mp3", ".m4a", ".aac", ".wav", ".amr":
		return "audio"
	}
	return "document"
}

// safeName turns a JID or an id into one path segment. Message ids come
// from the sender's client, so only letters, digits and . _ - @ are kept;
// anything else (a slash, a glob or shell character) is written as _xx, its
// hex code, which also keeps two different ids from sharing a name.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range []byte(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.' || r == '-' || r == '@':
			b.WriteByte(r)
		default:
			b.WriteString(fmt.Sprintf("_%02x", r))
		}
	}
	out := b.String()
	if out == "" || strings.Trim(out, ".") == "" {
		return "_" + out
	}
	return out
}

// instanceMediaDir is where one instance's downloads live.
func (s *Server) instanceMediaDir(instanceID string) string {
	return filepath.Join(s.mediaDir, safeName(instanceID))
}

// mediaFiles lists the downloaded files of one instance (all of them when
// instanceID is empty), optionally of one chat only.
func (s *Server) mediaFiles(instanceID, chat string) ([]mediaFile, error) {
	if s.mediaDir == "" {
		return nil, nil
	}
	root := s.mediaDir
	depth := 2 // <instance>/<chat>/file
	if instanceID != "" {
		root, depth = s.instanceMediaDir(instanceID), 1
		if chat != "" {
			root, depth = filepath.Join(root, safeName(chat)), 0
		}
	}
	var files []mediaFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipDir
			}
			return err
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") || strings.HasSuffix(d.Name(), ".part") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		chatOf := chat
		if depth > 0 && len(parts) > depth-1 {
			chatOf = parts[depth-1]
		}
		files = append(files, mediaFile{path: path, chat: chatOf, kind: mediaKind(path), size: info.Size(), modTime: info.ModTime()})
		return nil
	})
	if os.IsNotExist(err) {
		err = nil
	}
	return files, err
}

// retentionDays is how many days downloaded media is kept, 0 for forever.
func (s *Server) retentionDays(ctx context.Context) int {
	v, _ := s.index.Setting(ctx, RetentionSetting)
	n, _ := strconv.Atoi(v)
	return max(n, 0)
}

// MediaInventory is what the media folder holds.
type MediaInventory struct {
	Dir            string              `json:"dir"`
	Files          int                 `json:"files"`
	Bytes          int64               `json:"bytes"`
	ByType         map[string]MediaSum `json:"by_type"`
	ByChat         []ChatMediaSum      `json:"by_chat"`
	DuplicateBytes int64               `json:"duplicate_bytes"`
	Oldest         *time.Time          `json:"oldest_download,omitempty"`
	Newest         *time.Time          `json:"newest_download,omitempty"`
	RetentionDays  int                 `json:"retention_days"`
	// Exports are the files export_messages wrote, kept apart: the operator
	// asked for them, so retention never deletes them.
	Exports    MediaSum `json:"exports"`
	ExportsDir string   `json:"exports_dir"`
}

// MediaSum counts files and their size.
type MediaSum struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// ChatMediaSum is the downloads of one conversation.
type ChatMediaSum struct {
	ChatJID string `json:"chat_jid"`
	Name    string `json:"name,omitempty"`
	MediaSum
}

// Inventory measures the media folder of one instance (every instance when
// instanceID is empty): by type, by chat (largest first), and how much is the
// same file kept twice.
func (s *Server) Inventory(ctx context.Context, instanceID string) (MediaInventory, error) {
	files, err := s.mediaFiles(instanceID, "")
	if err != nil {
		return MediaInventory{}, err
	}
	inv := MediaInventory{Dir: s.mediaDir, ByType: map[string]MediaSum{}, ByChat: []ChatMediaSum{}, RetentionDays: s.retentionDays(ctx)}
	chats := map[string]*ChatMediaSum{}
	bySize := map[int64][]string{}
	for _, f := range files {
		inv.Files++
		inv.Bytes += f.size
		t := inv.ByType[f.kind]
		t.Files++
		t.Bytes += f.size
		inv.ByType[f.kind] = t
		c, ok := chats[f.chat]
		if !ok {
			c = &ChatMediaSum{ChatJID: f.chat}
			chats[f.chat] = c
		}
		c.Files++
		c.Bytes += f.size
		bySize[f.size] = append(bySize[f.size], f.path)
		mt := f.modTime.UTC()
		if inv.Oldest == nil || mt.Before(*inv.Oldest) {
			oldest := mt
			inv.Oldest = &oldest
		}
		if inv.Newest == nil || mt.After(*inv.Newest) {
			newest := mt
			inv.Newest = &newest
		}
	}
	// Only files of equal size can be equal, so only those are hashed.
	for size, paths := range bySize {
		if len(paths) < 2 || size == 0 {
			continue
		}
		seen := map[string]bool{}
		for _, p := range paths {
			h, err := hashFile(p)
			if err != nil {
				continue
			}
			if seen[h] {
				inv.DuplicateBytes += size
			}
			seen[h] = true
		}
	}
	for _, c := range chats {
		inv.ByChat = append(inv.ByChat, *c)
	}
	sort.Slice(inv.ByChat, func(i, j int) bool { return inv.ByChat[i].Bytes > inv.ByChat[j].Bytes })
	if len(inv.ByChat) > 20 {
		inv.ByChat = inv.ByChat[:20]
	}
	if instanceID != "" {
		for i := range inv.ByChat {
			inv.ByChat[i].Name = s.index.ChatName(ctx, instanceID, inv.ByChat[i].ChatJID)
		}
	}
	inv.ExportsDir = s.exportDir
	if s.exportDir != "" {
		if entries, err := os.ReadDir(s.exportDir); err == nil {
			for _, e := range entries {
				if info, err := e.Info(); err == nil && !e.IsDir() {
					inv.Exports.Files++
					inv.Exports.Bytes += info.Size()
				}
			}
		}
	}
	return inv, nil
}

// PurgeExports deletes the files export_messages wrote.
func (s *Server) PurgeExports() (files int, bytes int64, err error) {
	if s.exportDir == "" {
		return 0, 0, nil
	}
	entries, err := os.ReadDir(s.exportDir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || e.IsDir() {
			continue
		}
		if os.Remove(filepath.Join(s.exportDir, e.Name())) == nil {
			files++
			bytes += info.Size()
		}
	}
	return files, bytes, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Server) mediaStats(ctx context.Context, session Session) map[string]any {
	inv, err := s.Inventory(ctx, session.InstanceID)
	if err != nil {
		return toolError("%v", err)
	}
	return textResult(map[string]any{"inventory": inv,
		"note": "the files the tools downloaded on request, kept on the gateway's server; each can be downloaded again while WhatsApp holds it, so purge_media frees space without losing messages"}, false)
}

// PurgeMedia deletes downloaded files of one instance (every instance when
// instanceID is empty) older than days (0: any age), of one chat or all, of at
// least minBytes, and reports what it removed. With dryRun it only reports
// what it would remove.
func (s *Server) PurgeMedia(instanceID, chat string, days int, minBytes int64, dryRun bool) (files int, bytes int64, largest []map[string]any, err error) {
	all, err := s.mediaFiles(instanceID, chat)
	if err != nil {
		return 0, 0, nil, err
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	var picked []mediaFile
	for _, f := range all {
		if days > 0 && f.modTime.After(cutoff) {
			continue
		}
		if f.size < minBytes {
			continue
		}
		picked = append(picked, f)
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].size > picked[j].size })
	for i, f := range picked {
		if i < 10 {
			largest = append(largest, map[string]any{"chat_jid": f.chat, "file": filepath.Base(f.path), "bytes": f.size, "downloaded_at": f.modTime.UTC()})
		}
		if !dryRun {
			if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
				continue
			}
			_ = os.Remove(filepath.Dir(f.path)) // only succeeds once the chat's folder is empty
		}
		files++
		bytes += f.size
	}
	return files, bytes, largest, nil
}

func (s *Server) purgeMedia(_ context.Context, session Session, a arguments) map[string]any {
	if a.OlderThan < 0 || a.MinMegabyte < 0 {
		return toolError("older_than_days and min_megabytes must be positive")
	}
	files, bytes, largest, err := s.PurgeMedia(session.InstanceID, strings.TrimSpace(a.ChatJID), a.OlderThan, int64(a.MinMegabyte)<<20, !a.Confirm)
	if err != nil {
		return toolError("%v", err)
	}
	if !a.Confirm {
		return textResult(map[string]any{"preview": true, "would_delete_files": files, "would_free_bytes": bytes, "largest": largest,
			"next": "call purge_media again with the same filters and confirm true to delete them; each file can be downloaded again while WhatsApp holds it"}, false)
	}
	return textResult(map[string]any{"deleted_files": files, "freed_bytes": bytes}, false)
}

// SweepMedia applies the retention setting: files downloaded more days ago
// than it allows are deleted. It does nothing while retention is off.
func (s *Server) SweepMedia(ctx context.Context) (int, int64, error) {
	days := s.retentionDays(ctx)
	if days <= 0 {
		return 0, 0, nil
	}
	files, bytes, _, err := s.PurgeMedia("", "", days, 0, false)
	if err == nil && files > 0 && s.logger != nil {
		s.logger.Info("media retention", "deleted_files", files, "freed_bytes", bytes, "days", days)
	}
	return files, bytes, err
}

// RunRetention applies the retention setting now and then every hour, until
// ctx ends.
func (s *Server) RunRetention(ctx context.Context) {
	for {
		_, _, _ = s.SweepMedia(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}

// SetRetention changes how many days downloaded media is kept; 0 keeps it.
func (s *Server) SetRetention(ctx context.Context, days int) error {
	if days <= 0 {
		return s.index.SetSetting(ctx, RetentionSetting, "")
	}
	return s.index.SetSetting(ctx, RetentionSetting, strconv.Itoa(days))
}

// cachedMedia finds a file already downloaded for a message.
func (s *Server) cachedMedia(instanceID, chat, messageID string) (string, bool) {
	if s.mediaDir == "" {
		return "", false
	}
	dir := filepath.Join(s.instanceMediaDir(instanceID), safeName(chat))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	prefix := safeName(messageID) + "."
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, prefix) || strings.HasPrefix(name, ".") || strings.Contains(name[len(prefix):], ".") {
			continue // a half-written download, or another id that shares the prefix
		}
		if info, err := e.Info(); err == nil && info.Size() > 0 {
			return filepath.Join(dir, name), true
		}
	}
	return "", false
}

// keepMedia saves a downloaded file into the media folder and says where.
func (s *Server) keepMedia(instanceID, chat, messageID, mimeType string, data []byte) (string, error) {
	if s.mediaDir == "" {
		return "", errors.New("no media folder is configured")
	}
	dir := filepath.Join(s.instanceMediaDir(instanceID), safeName(chat))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(dir, safeName(messageID)+extensionOr(mimeType, ".bin"))
	// Written under a hidden temporary name first, so a download that fails
	// halfway is never served as the file, and two at once do not collide.
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o640)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return path, nil
}

func extensionOr(mimeType, fallback string) string {
	if ext := extension(mimeType); ext != "" {
		return ext
	}
	return fallback
}
