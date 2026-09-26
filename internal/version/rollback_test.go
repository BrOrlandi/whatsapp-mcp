package version

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

type memoryMark struct{ highest string }

func (m *memoryMark) HighestVersion(context.Context) (string, error) { return m.highest, nil }
func (m *memoryMark) SaveHighestVersion(_ context.Context, v string) error {
	m.highest = v
	return nil
}

// The mark only rises, and starting below it is reported rather than hidden.
func TestCheckRollbackRaisesTheMarkAndReportsGoingBack(t *testing.T) {
	previous := Version
	t.Cleanup(func() { Version = previous; rolledBackFrom.Store(nil) })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mark := &memoryMark{}

	Version = "v0.3.0-beta.1"
	CheckRollback(context.Background(), mark, logger)
	if mark.highest != "0.3.0-beta.1" || RolledBackFrom() != "" {
		t.Fatalf("first run: %q %q", mark.highest, RolledBackFrom())
	}
	Version = "v0.3.0-beta.3-4-gabc1234"
	CheckRollback(context.Background(), mark, logger)
	if mark.highest != "0.3.0-beta.3" {
		t.Fatalf("forward: %q", mark.highest)
	}
	Version = "v0.3.0-beta.2"
	CheckRollback(context.Background(), mark, logger)
	if mark.highest != "0.3.0-beta.3" || RolledBackFrom() != "0.3.0-beta.3" {
		t.Fatalf("rollback: mark %q, from %q", mark.highest, RolledBackFrom())
	}
	Version = "v0.3.0-beta.3"
	CheckRollback(context.Background(), mark, logger)
	if RolledBackFrom() != "" {
		t.Fatal("the warning outlived the rollback")
	}
	Version = "dev"
	CheckRollback(context.Background(), mark, logger)
	if mark.highest != "0.3.0-beta.3" {
		t.Fatal("a dev build moved the mark")
	}
}
