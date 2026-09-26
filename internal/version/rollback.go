package version

import (
	"context"
	"log/slog"
	"sync/atomic"
)

// rolledBackFrom is the newer release this database has already run, when the
// one running now is older. Empty in every ordinary case.
var rolledBackFrom atomic.Pointer[string]

// RolledBackFrom reports the newer release this database was last moved to,
// when the running build is older than it; "" otherwise.
func RolledBackFrom() string {
	if from := rolledBackFrom.Load(); from != nil {
		return *from
	}
	return ""
}

// HighWater is where the newest version seen is kept.
type HighWater interface {
	HighestVersion(context.Context) (string, error)
	SaveHighestVersion(context.Context, string) error
}

// CheckRollback compares the running release against the newest one this
// database has seen. Moving forward raises the mark; starting below it is a
// rollback, which is remembered for the panel and logged, and does not stop
// anything: the operator may have good reasons, and a gateway that refuses to
// start is a worse outcome than one that warns.
//
// A build with no release in its history — "dev", a fresh clone — says
// nothing either way, and leaves the mark alone.
func CheckRollback(ctx context.Context, store HighWater, logger *slog.Logger) {
	running := Release()
	if running == "" {
		return
	}
	highest, err := store.HighestVersion(ctx)
	if err != nil {
		logger.Warn("could not read the newest version this database has run", "error", err)
		return
	}
	switch {
	case highest == "" || Compare(running, highest) > 0:
		if err := store.SaveHighestVersion(ctx, running); err != nil {
			logger.Warn("could not record the running version", "error", err)
		}
		rolledBackFrom.Store(nil)
	case Compare(running, highest) < 0:
		rolledBackFrom.Store(&highest)
		logger.Warn("this build is older than one that already ran against this database; migrations do not run backwards, so an older build may not understand the schema", "running", running, "highest", highest)
	default:
		rolledBackFrom.Store(nil)
	}
}
