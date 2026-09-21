// Package version answers two questions an operator asks about a running
// instance: what is this, and is there a newer one.
//
// The number itself is stamped at build time, so it lives in a package
// variable — that is the only shape the linker's -X flag can write to. The
// rest of this package exists so that everything which needs to say the
// version says the same thing: the panel footer, the health probes, the CLI.
package version

import (
	"strconv"
	"strings"
)

// Version is stamped at build time with
//
//	-ldflags "-X github.com/BrOrlandi/whatsapp-mcp/internal/version.Version=$(git describe --tags --always --dirty)"
//
// which yields "0.1.0-beta" exactly on a release tag, "0.1.0-beta-12-gabc1234"
// twelve commits past it, and a bare short sha in a repository with no tags at
// all. "dev" is the honest answer for a build that went through none of that.
var Version = "dev"

// String is the version as it should be printed: the describe output, with the
// leading "v" of a git tag removed. The tag carries the v because that is the
// git convention; the number does not, because that is the semver one.
func String() string {
	return strings.TrimPrefix(strings.TrimSpace(Version), "v")
}

// Release is the released version this build descends from — the tag part of a
// describe string, with the commit count and the sha dropped. It is what gets
// compared against the latest published release, since "0.1.0-beta-12-gabc1234"
// and "0.1.0-beta" are the same release as far as "is there a newer one" goes.
//
// It is empty when this build descends from no tag at all, which is the state
// of a fresh clone before the first release and of any "dev" build.
func Release() string {
	v := String()
	if v == "" || v == "dev" {
		return ""
	}
	// A dirty working tree is not a release, but it still descends from one:
	// the suffix goes, the tag underneath it stays. Development is what reports
	// that this build is not the tag it names.
	v = strings.TrimSuffix(v, "-dirty")
	// A describe string ends in "-<commits>-g<sha>". Both trailing fields have
	// to be there, and the sha field has to start with g, or this is a plain
	// tag that happens to contain hyphens — which every prerelease does.
	parts := strings.Split(v, "-")
	if len(parts) >= 3 {
		last, count := parts[len(parts)-1], parts[len(parts)-2]
		if strings.HasPrefix(last, "g") && isDigits(count) {
			v = strings.Join(parts[:len(parts)-2], "-")
		}
	}
	if !looksLikeVersion(v) {
		return ""
	}
	return v
}

// Development reports whether this build is anything other than a published
// release sitting exactly on its tag: a local build, a commit past the tag, or
// a working tree with uncommitted changes. The update check stays quiet for
// these, because "newer release available" is not useful to someone running
// code that is ahead of every release.
func Development() string {
	v := String()
	if v == "" || v == "dev" {
		return "dev"
	}
	if strings.HasSuffix(v, "-dirty") || Release() != v {
		return v
	}
	return ""
}

// Compare orders two versions by semantic-versioning precedence and returns
// -1, 0 or 1. Numeric fields compare as numbers, so 0.10.0 is above 0.9.0; a
// prerelease is below the release it prefixes, so 0.1.0-beta is below 0.1.0.
// Anything unparseable compares as equal rather than as older, because the
// only caller is an update banner and a banner raised on a string nobody can
// read is worse than no banner.
func Compare(a, b string) int {
	an, ap, aok := parse(a)
	bn, bp, bok := parse(b)
	if !aok || !bok {
		return 0
	}
	for i := range an {
		if an[i] != bn[i] {
			return sign(an[i] - bn[i])
		}
	}
	switch {
	case ap == "" && bp == "":
		return 0
	// A version with a prerelease is below the same version without one.
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return comparePrerelease(ap, bp)
}

// comparePrerelease applies the dot-separated rules from the semver spec:
// numeric identifiers compare numerically and rank below alphanumeric ones,
// and a shorter run of identifiers ranks below a longer one that starts the
// same way. So beta.2 is above beta.1, and beta is below beta.1.
func comparePrerelease(a, b string) int {
	af, bf := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(af) && i < len(bf); i++ {
		if af[i] == bf[i] {
			continue
		}
		an, aErr := strconv.Atoi(af[i])
		bn, bErr := strconv.Atoi(bf[i])
		switch {
		case aErr == nil && bErr == nil:
			return sign(an - bn)
		// A numeric identifier always ranks below an alphanumeric one.
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		}
		return strings.Compare(af[i], bf[i])
	}
	return sign(len(af) - len(bf))
}

// parse splits "1.2.3-beta.1+build" into its numeric fields and its
// prerelease. Build metadata is dropped: the spec says it takes no part in
// precedence. A missing minor or patch reads as zero, so "1" and "1.0.0" are
// the same version.
func parse(v string) ([3]int, string, bool) {
	var numbers [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return numbers, "", false
	}
	if plus := strings.IndexByte(v, '+'); plus >= 0 {
		v = v[:plus]
	}
	pre := ""
	if dash := strings.IndexByte(v, '-'); dash >= 0 {
		pre, v = v[dash+1:], v[:dash]
	}
	fields := strings.Split(v, ".")
	if len(fields) > 3 {
		return numbers, "", false
	}
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return numbers, "", false
		}
		numbers[i] = n
	}
	return numbers, pre, true
}

func looksLikeVersion(v string) bool {
	_, _, ok := parse(v)
	return ok
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
