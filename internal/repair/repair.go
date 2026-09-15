// Package repair rebuilds the message index from the events already stored.
//
// Every delivery is kept as the raw bytes RabbitMQ carried, which means a
// decoder that misread them once is not a loss of data: the events are intact
// and can be read again. That is the difference this package exists to exploit.
// A window the index appears to be missing is not necessarily a window WhatsApp
// has to resend — it may be a window that was received, stored, and merely
// projected badly, and asking WhatsApp for it again would be both slower and
// less complete than reading what is already on disk.
package repair

import (
	"context"
	"log/slog"

	"github.com/BrOrlandi/whatsapp-mcp/internal/events"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// projection is the slice of the store a repair pass needs. Narrowing it keeps
// the walk testable without a database, which is where the termination and
// dry-run guarantees actually get checked.
type projection interface {
	UnprojectedEvents(context.Context, int) ([]store.PendingEvent, error)
	Reproject(context.Context, string, []store.Message) (int, error)
	OrphanRows(context.Context) (int64, error)
}

// Report is what a repair pass did. Orphans are counted from the database
// before and after rather than inferred from the loop's own arithmetic, so a
// pass that believes it succeeded while the rows are still unreadable says so.
type Report struct {
	Events        int   `json:"events"`
	Messages      int   `json:"messages"`
	Undecodable   int   `json:"undecodable"`
	OrphansBefore int64 `json:"orphans_before"`
	OrphansAfter  int64 `json:"orphans_after"`
	DryRun        bool  `json:"dry_run"`
}

// Reproject re-decodes every message delivery whose rows are unreadable and
// writes the result back.
//
// A dry run reads and decodes exactly as the real pass does but writes nothing,
// which is the only honest way to size a production data change before making
// it: the count it reports is produced by the same code that would do the work.
func Reproject(ctx context.Context, db *store.Store, batch int, dryRun bool, logger *slog.Logger) (Report, error) {
	return reproject(ctx, db, batch, dryRun, logger)
}

func reproject(ctx context.Context, db projection, batch int, dryRun bool, logger *slog.Logger) (Report, error) {
	if logger == nil {
		logger = slog.Default()
	}
	report := Report{DryRun: dryRun}
	before, err := db.OrphanRows(ctx)
	if err != nil {
		return report, err
	}
	report.OrphansBefore = before

	// An event that decodes to nothing stays unprojected, so it would be handed
	// back by every later page and the loop would never end. Remembering what
	// has been seen is what makes the walk terminate on its own rather than on
	// a guessed iteration cap.
	seen := make(map[string]bool)
	for {
		pending, err := db.UnprojectedEvents(ctx, batch)
		if err != nil {
			return report, err
		}
		fresh := 0
		for _, event := range pending {
			if seen[event.ID] {
				continue
			}
			seen[event.ID] = true
			fresh++
			decoded, err := events.Decode(event.Payload)
			if err != nil || len(decoded.Record.Messages) == 0 {
				report.Undecodable++
				logger.Debug("event still unreadable", "event_id", event.ID, "type", event.Type)
				continue
			}
			report.Events++
			if dryRun {
				report.Messages += len(decoded.Record.Messages)
				continue
			}
			written, err := db.Reproject(ctx, event.ID, decoded.Record.Messages)
			if err != nil {
				return report, err
			}
			report.Messages += written
		}
		if fresh == 0 {
			break
		}
	}

	after, err := db.OrphanRows(ctx)
	if err != nil {
		return report, err
	}
	report.OrphansAfter = after
	return report, nil
}

// RunIfStale rebuilds the projection when the decoder has moved on since the
// rows were written, and does nothing otherwise.
//
// This is what makes a decoder fix self-healing: correcting Decode and raising
// events.DecoderVersion is enough, and the next deploy repairs the history it
// had misread. Without it, every such fix silently leaves everything received
// before it unreadable, and only someone who happened to notice would ever go
// back for it.
//
// It runs in the background because a large backlog takes longer than a probe
// will wait, and the gateway is useful for live traffic while it works.
func RunIfStale(ctx context.Context, db *store.Store, progress func(Report, bool), logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	stamped, err := db.DecoderVersion(ctx)
	if err != nil {
		logger.Error("could not read the decoder version; skipping reprojection", "error", err)
		return
	}
	if stamped >= events.DecoderVersion {
		return
	}
	logger.Info("message projection is behind the decoder; rebuilding it",
		"stamped_version", stamped, "decoder_version", events.DecoderVersion)
	if progress != nil {
		progress(Report{}, true)
	}

	report, err := Reproject(ctx, db, 500, false, logger)
	if progress != nil {
		progress(report, false)
	}
	if err != nil {
		// The version is deliberately left alone: an interrupted pass must be
		// retried on the next start rather than recorded as done.
		logger.Error("reprojection failed; it will be retried on the next start", "error", err,
			"events", report.Events, "messages", report.Messages)
		return
	}
	if err := db.MarkReprojected(ctx, events.DecoderVersion, int64(report.Events), int64(report.Messages)); err != nil {
		logger.Error("reprojection finished but could not be recorded", "error", err)
		return
	}
	logger.Info("message projection rebuilt",
		"events", report.Events, "messages", report.Messages, "undecodable", report.Undecodable,
		"orphans_before", report.OrphansBefore, "orphans_after", report.OrphansAfter)
}
