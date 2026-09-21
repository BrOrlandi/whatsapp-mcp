package version

import "testing"

func TestStringAndRelease(t *testing.T) {
	cases := []struct {
		stamped     string
		want        string
		release     string
		development string
	}{
		// A published release, which is the only state the update banner may
		// fire in.
		{stamped: "v0.1.0-beta", want: "0.1.0-beta", release: "0.1.0-beta", development: ""},
		{stamped: "0.1.0-beta", want: "0.1.0-beta", release: "0.1.0-beta", development: ""},
		{stamped: "v1.2.3", want: "1.2.3", release: "1.2.3", development: ""},
		// Past the tag: a describe string, whose trailing "-12-gabc1234" is
		// what separates it from a prerelease that merely contains hyphens.
		{stamped: "v0.1.0-beta-12-gabc1234", want: "0.1.0-beta-12-gabc1234", release: "0.1.0-beta", development: "0.1.0-beta-12-gabc1234"},
		{stamped: "v1.2.3-4-gdeadbee", want: "1.2.3-4-gdeadbee", release: "1.2.3", development: "1.2.3-4-gdeadbee"},
		// Uncommitted changes on top of a tag descend from it without being
		// it: the release underneath is named, and Development says why the
		// banner must stay quiet.
		{stamped: "v1.2.3-dirty", want: "1.2.3-dirty", release: "1.2.3", development: "1.2.3-dirty"},
		{stamped: "v0.1.0-beta-3-gabc1234-dirty", want: "0.1.0-beta-3-gabc1234-dirty", release: "0.1.0-beta", development: "0.1.0-beta-3-gabc1234-dirty"},
		// No tag to describe from, and no build stamp at all.
		{stamped: "abc1234", want: "abc1234", release: "", development: "abc1234"},
		{stamped: "dev", want: "dev", release: "", development: "dev"},
	}
	for _, c := range cases {
		t.Run(c.stamped, func(t *testing.T) {
			defer stamp(t, c.stamped)()
			if got := String(); got != c.want {
				t.Errorf("String() = %q, want %q", got, c.want)
			}
			if got := Release(); got != c.release {
				t.Errorf("Release() = %q, want %q", got, c.release)
			}
			if got := Development(); got != c.development {
				t.Errorf("Development() = %q, want %q", got, c.development)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		// Numeric fields compare as numbers, not as text.
		{"0.10.0", "0.9.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		// A prerelease is below the release it prefixes. This is the rule the
		// whole beta series depends on.
		{"0.1.0-beta", "0.1.0", -1},
		{"0.1.0", "0.1.0-beta", 1},
		{"0.1.0-beta", "0.2.0-beta", -1},
		// Prerelease identifiers compare field by field.
		{"0.1.0-beta.1", "0.1.0-beta.2", -1},
		{"0.1.0-beta", "0.1.0-beta.1", -1},
		{"0.1.0-alpha", "0.1.0-beta", -1},
		{"0.1.0-rc.1", "0.1.0-beta.9", 1},
		// Build metadata takes no part in precedence.
		{"1.0.0+build.9", "1.0.0+build.1", 0},
		// Missing fields read as zero.
		{"1", "1.0.0", 0},
		// Anything unreadable compares equal, so no banner is raised on it.
		{"dev", "0.1.0", 0},
		{"0.1.0", "not-a-version", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLatest(t *testing.T) {
	t.Run("silent before any check has succeeded", func(t *testing.T) {
		defer stamp(t, "v0.1.0-beta")()
		defer publish(t, "")()
		if tag, behind := Latest(); tag != "" || behind {
			t.Errorf("Latest() = %q, %v; want the panel to say nothing", tag, behind)
		}
	})
	t.Run("behind a newer release", func(t *testing.T) {
		defer stamp(t, "v0.1.0-beta")()
		defer publish(t, "0.2.0")()
		tag, behind := Latest()
		if tag != "0.2.0" || !behind {
			t.Errorf("Latest() = %q, %v; want 0.2.0, true", tag, behind)
		}
	})
	t.Run("current", func(t *testing.T) {
		defer stamp(t, "v0.2.0")()
		defer publish(t, "0.2.0")()
		if _, behind := Latest(); behind {
			t.Error("a release sitting on the newest tag is not behind it")
		}
	})
	// A build past the tag is ahead of every release, so telling its operator
	// to update would walk them backwards.
	t.Run("a build past the newest tag", func(t *testing.T) {
		defer stamp(t, "v0.2.0-7-gabc1234")()
		defer publish(t, "0.2.0")()
		if _, behind := Latest(); behind {
			t.Error("a build past the newest release is ahead of it, not behind")
		}
	})
	t.Run("a local build", func(t *testing.T) {
		defer stamp(t, "dev")()
		defer publish(t, "9.9.9")()
		if _, behind := Latest(); behind {
			t.Error("a dev build has no release to be behind")
		}
	})
}

// stamp replaces the build-time version for one test and restores it.
func stamp(t *testing.T, v string) func() {
	t.Helper()
	previous := Version
	Version = v
	return func() { Version = previous }
}

// publish sets what the last update check found, standing in for GitHub.
func publish(t *testing.T, tag string) func() {
	t.Helper()
	previous := latest.Load()
	Record(tag)
	return func() { latest.Store(previous) }
}
