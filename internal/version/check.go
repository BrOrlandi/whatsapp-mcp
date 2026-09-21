package version

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// The update check asks GitHub what the newest published release is, so the
// panel can tell an operator that their instance is behind and hand them the
// command that fixes it. Nothing about the instance is sent: it is an
// unauthenticated GET of a public list, and the answer is the only thing that
// crosses back.
//
// /releases/latest is deliberately not the endpoint. It excludes prereleases,
// and the whole 0.x series here is prereleases — it would answer 404 for the
// entire beta. The list endpoint returns everything, newest first.
const (
	checkInterval = 6 * time.Hour
	checkTimeout  = 10 * time.Second
)

// releasesURL is a variable only so a test can point it at its own server.
var releasesURL = "https://api.github.com/repos/BrOrlandi/whatsapp-mcp/releases?per_page=10"

// latest holds the newest release tag seen so far, or "" when no check has
// succeeded yet. It is a package variable for the same reason Version is: the
// panel's template functions are a package-level map, and the alternative was
// threading a checker through every page's data struct to render one line in
// the footer.
var latest atomic.Pointer[string]

// Record stores what a check found. The poller calls it; so does anything that
// learns the newest release by another route, which in practice is a test.
func Record(tag string) { latest.Store(&tag) }

// Latest reports the newest published release and whether this instance is
// behind it. It never blocks and never fails: before the first successful
// check, and whenever GitHub is unreachable, it simply reports that there is
// nothing to say. A panel page must render on a server with no outbound
// network.
func Latest() (string, bool) {
	tag := latest.Load()
	if tag == nil || *tag == "" {
		return "", false
	}
	// A build that is not sitting exactly on a release tag is ahead of every
	// published release, not behind one. Telling its operator to "update" would
	// walk them backwards.
	if Development() != "" {
		return *tag, false
	}
	return *tag, Compare(Release(), *tag) < 0
}

// StartUpdateCheck polls for new releases until the context ends. It is
// started once, from main, and is a no-op when the operator has turned it off:
// this reaches out from their server, so it has to be one variable to stop.
func StartUpdateCheck(ctx context.Context, enabled bool, logger *slog.Logger) {
	if !enabled {
		return
	}
	go func() {
		// The first check waits a little: an instance that has just started is
		// busy connecting to its own dependencies, and nothing here is urgent.
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			if tag, err := fetchLatest(ctx); err != nil {
				// A failed check is not a problem with this instance, so it is
				// noted and forgotten rather than reported as a fault. The last
				// good answer stands.
				logger.Debug("could not check for a newer release", "error", err)
			} else if tag != "" {
				Record(tag)
			}
			timer.Reset(checkInterval)
		}
	}()
}

// fetchLatest returns the newest non-draft release tag. Drafts are skipped
// because they are not published to anyone; prereleases are kept, because
// during the beta they are all there is.
func fetchLatest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "whatsapp-mcp/"+String())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", &statusError{response.StatusCode}
	}
	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(response.Body).Decode(&releases); err != nil {
		return "", err
	}
	for _, release := range releases {
		if release.Draft {
			continue
		}
		if tag := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v"); tag != "" {
			return tag, nil
		}
	}
	return "", nil
}

type statusError struct{ code int }

func (e *statusError) Error() string { return "github answered " + http.StatusText(e.code) }
