package repair

import (
	"context"
	"errors"
	"testing"

	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

// A payload the old decoder could not attribute: whatsmeow's own shape, which
// is what Evolution Go actually publishes.
const livePayload = `{"event":"Message","instanceId":"inst-1","data":{
  "Info":{"ID":"MSG1","Chat":"a@s.whatsapp.net","Sender":"b@s.whatsapp.net","PushName":"Fulano","Timestamp":"2026-09-14T10:00:00Z"},
  "Message":{"conversation":"mensagem perdida"}}}`

// Media without a caption produced no row at all under the old decoder, so it
// left no orphan behind to find it by.
const mediaPayload = `{"event":"Message","instanceId":"inst-1","data":{
  "Info":{"ID":"MSG2","Chat":"a@s.whatsapp.net","Timestamp":"2026-09-14T10:05:00Z"},
  "Message":{"audioMessage":{"seconds":7}}}}`

type fakeDB struct {
	pending   []store.PendingEvent
	written   map[string][]store.Message
	orphans   int64
	failWrite bool
}

func (f *fakeDB) UnprojectedEvents(_ context.Context, limit int) ([]store.PendingEvent, error) {
	return f.pending, nil
}
func (f *fakeDB) Reproject(_ context.Context, eventID string, messages []store.Message) (int, error) {
	if f.failWrite {
		return 0, errors.New("database is down")
	}
	if f.written == nil {
		f.written = map[string][]store.Message{}
	}
	f.written[eventID] = messages
	// A repaired event no longer has an orphan behind it.
	if f.orphans > 0 {
		f.orphans--
	}
	return len(messages), nil
}
func (f *fakeDB) OrphanRows(context.Context) (int64, error) { return f.orphans, nil }

// The walk has to terminate on its own. An event that decodes to nothing stays
// unprojected, so the query hands it back on every page: without remembering
// what it has seen, the loop would run forever.
func TestReprojectTerminatesOnUndecodableEvents(t *testing.T) {
	db := &fakeDB{pending: []store.PendingEvent{
		{ID: "e1", Type: "Message", Payload: []byte(`{"event":"Message","instanceId":"i","data":{"Info":{},"Message":{}}}`)},
	}}
	report, err := reproject(context.Background(), db, 10, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Undecodable != 1 || report.Events != 0 {
		t.Fatalf("report = %+v", report)
	}
}

// A dry run must read and decode exactly as the real pass does, and write
// nothing: that is the only way its count can be trusted to size the change.
func TestDryRunCountsWithoutWriting(t *testing.T) {
	db := &fakeDB{orphans: 1, pending: []store.PendingEvent{
		{ID: "e1", Type: "Message", Payload: []byte(livePayload)},
	}}
	report, err := reproject(context.Background(), db, 10, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Events != 1 || report.Messages != 1 {
		t.Fatalf("report = %+v", report)
	}
	if len(db.written) != 0 {
		t.Fatalf("a dry run wrote %v", db.written)
	}
	if report.OrphansAfter != 1 {
		t.Fatalf("a dry run changed the orphan count: %+v", report)
	}
}

// The repair covers deliveries that produced no row at all, not only the ones
// that left an orphan — that is why the selection looks for a missing good row.
func TestReprojectRepairsBothOrphanedAndMissingRows(t *testing.T) {
	db := &fakeDB{orphans: 2, pending: []store.PendingEvent{
		{ID: "e1", Type: "Message", Payload: []byte(livePayload)},
		{ID: "e2", Type: "Message", Payload: []byte(mediaPayload)},
	}}
	report, err := reproject(context.Background(), db, 10, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Events != 2 || report.Messages != 2 {
		t.Fatalf("report = %+v", report)
	}
	text := db.written["e1"][0]
	if text.InstanceID != "inst-1" || text.ChatJID != "a@s.whatsapp.net" || text.Text != "mensagem perdida" || text.SentAt.IsZero() {
		t.Fatalf("repaired text message = %+v", text)
	}
	media := db.written["e2"][0]
	if media.MediaType != "audio" || media.MessageID != "MSG2" {
		t.Fatalf("repaired media message = %+v", media)
	}
}

// Orphans are counted from the database on both sides, so a pass that wrote
// rows but left the unreadable ones in place reports that rather than success.
func TestReportCountsOrphansFromTheDatabase(t *testing.T) {
	db := &fakeDB{orphans: 5, pending: []store.PendingEvent{
		{ID: "e1", Type: "Message", Payload: []byte(livePayload)},
	}}
	report, err := reproject(context.Background(), db, 10, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.OrphansBefore != 5 || report.OrphansAfter != 4 {
		t.Fatalf("report = %+v", report)
	}
}

// A failed write must surface. Recording the pass as done would leave the rows
// unreadable with nothing scheduled to try again.
func TestWriteFailureIsReported(t *testing.T) {
	db := &fakeDB{failWrite: true, pending: []store.PendingEvent{
		{ID: "e1", Type: "Message", Payload: []byte(livePayload)},
	}}
	if _, err := reproject(context.Background(), db, 10, false, nil); err == nil {
		t.Fatal("a failed write was reported as success")
	}
}
