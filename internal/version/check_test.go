package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLatest(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			// The reason this asks for the list instead of /releases/latest:
			// that endpoint hides prereleases, and the whole 0.1.0-beta series
			// is prereleases. It would answer 404 for the entire beta.
			name: "a prerelease counts",
			body: `[{"tag_name":"v0.1.0-beta","draft":false,"prerelease":true}]`,
			want: "0.1.0-beta",
		},
		{
			// A draft is published to nobody, so telling an operator to update
			// to it would name a version they cannot get.
			name: "a draft does not",
			body: `[{"tag_name":"v0.3.0","draft":true},{"tag_name":"v0.2.0","draft":false}]`,
			want: "0.2.0",
		},
		{
			name: "nothing published yet",
			body: `[]`,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(c.body))
			}))
			defer server.Close()
			defer point(t, server.URL)()
			got, err := fetchLatest(context.Background())
			if err != nil {
				t.Fatalf("fetchLatest: %v", err)
			}
			if got != c.want {
				t.Errorf("fetchLatest() = %q, want %q", got, c.want)
			}
		})
	}
}

// A check that cannot reach GitHub must leave the last good answer standing
// and report an error, never a version.
func TestFetchLatestRefuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	defer point(t, server.URL)()
	if got, err := fetchLatest(context.Background()); err == nil {
		t.Errorf("fetchLatest() = %q, nil; want an error on a rate-limited API", got)
	}
}

func point(t *testing.T, url string) func() {
	t.Helper()
	previous := releasesURL
	releasesURL = url
	return func() { releasesURL = previous }
}
