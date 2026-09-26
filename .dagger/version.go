// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// This file decides the two things the archetype leaves to its caller: which
// version a build is, and whether it is published.
//
// Both used to be the module's. GoApp read the refs at HEAD, stamped the binary
// with the tag pointing at it or with `<short-sha>-<commit-time>` where none
// did, and published when a ref matched a publishOn regex. The Go chain that
// replaced it takes a version from its caller and publishes when it is told to,
// so the same rules are written down here instead — unchanged, because
// docs/publishing.md and docs/versioning.md describe them to consumers and the
// retention workflow matches on the tags they produce.
//
// VersionScheme runs both over a table of literal cases on every pull request,
// which is what makes the rules a thing this repository checks rather than a
// thing it intends. A version or a publish decision that is wrong is otherwise
// discovered afterwards, by somebody who pulled the image.
package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"dagger/dfcad/internal/dagger"
)

// ociTag is the charset an image tag may be spelled in. A version outside it
// cannot be published, and the archetype refuses it; so does planVersion, first,
// so the refusal is the same whether or not the build was going to publish.
var ociTag = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// Version is the version this build is stamped with and, when it publishes, the
// tag it is published under.
//
// A tag pointing at HEAD is the version, verbatim; where more than one does, the
// most recently created. Anything else is `<short-sha>-<commit-time>`, the
// commit's committer time with its colons and any `+` made tag-safe, so a
// rebuild of one commit is the same version rather than a new one.
//
// +cache="session"
func (m *Dfcad) Version(ctx context.Context) (string, error) {
	refs, err := headRefs(ctx, m.Source, m.GitDir)
	if err != nil {
		return "", err
	}
	shortSHA, committed, err := headCommit(ctx, m.Source, m.GitDir)
	if err != nil {
		return "", err
	}
	return planVersion(refs, shortSHA, committed)
}

// Publish builds the image for every platform and, when a ref at HEAD matches
// publishOn, pushes it.
//
// A build whose refs do not match is built and thrown away, and that is the
// answer rather than a fault: a pull request's merge commit is on no branch and
// carries no tag, so it proves the image builds on both platforms and publishes
// nothing. The decision is publishOn's and the refs', not the caller's — which is
// what keeps "does a pull request publish?" a question with one answer written
// down in one place.
//
// The credentials are only required of a build that publishes, and every one of
// them is checked before the first byte moves: a publish that reached the
// registry and then found it could not sign would be a failed release rather
// than a refused one.
//
// +cache="never"
func (m *Dfcad) Publish(
	ctx context.Context,
	// The refs that publish, as a regular expression over the refs at HEAD —
	// `^refs/(heads/main|tags/v.+)$`. A remote-tracking branch counts as the
	// branch it tracks.
	publishOn string,
	// The registry, without a repository path — `ghcr.io`.
	//
	// +optional
	registry string,
	// The image's repository within that registry, without a tag —
	// `z5labs/dfcad`.
	//
	// +optional
	repository string,
	// The registry username to authenticate as.
	//
	// +optional
	username string,
	// The registry password or token to authenticate with.
	//
	// +optional
	password *dagger.Secret,
	// The CI provider's OIDC token request endpoint —
	// `ACTIONS_ID_TOKEN_REQUEST_URL` on GitHub Actions. Every published image is
	// signed, and signing exchanges a token from it for the identity that signs.
	//
	// +optional
	idTokenRequestUrl string,
	// The bearer token for that endpoint — `ACTIONS_ID_TOKEN_REQUEST_TOKEN` on
	// GitHub Actions. A secret, because it mints identity tokens.
	//
	// +optional
	idTokenRequestToken *dagger.Secret,
) (string, error) {
	refs, err := headRefs(ctx, m.Source, m.GitDir)
	if err != nil {
		return "", err
	}
	publishes, err := matchesPublishOn(refs, publishOn)
	if err != nil {
		return "", err
	}
	version, err := m.Version(ctx)
	if err != nil {
		return "", err
	}

	app := m.app(version)
	containers, err := app.Containers(ctx)
	if err != nil {
		return "", fmt.Errorf("building %s: %w", version, err)
	}
	for i := range containers {
		if _, err := containers[i].Sync(ctx); err != nil {
			return "", fmt.Errorf("building %s: %w", version, err)
		}
	}

	if !publishes {
		return fmt.Sprintf("built %s; not published: no ref at HEAD matches %s (refs: %s)\n",
			version, publishOn, strings.Join(refs, ", ")), nil
	}

	switch {
	case registry == "":
		return "", errors.New("registry is required to publish: it is the registry alone, such as `ghcr.io`, and never a repository path")
	case repository == "":
		return "", errors.New("repository is required to publish: it is the image's path within the registry, without a tag")
	case username == "" || password == nil:
		return "", errors.New("username and password are both required to publish: an image is pushed to a registry that authenticates")
	case idTokenRequestUrl == "" || idTokenRequestToken == nil:
		return "", errors.New("idTokenRequestUrl and idTokenRequestToken are both required to publish: every published image is signed, and signing exchanges a workload identity token")
	}

	published, err := app.
		WithRegistry(registry, username, password).
		WithOidc(idTokenRequestUrl, idTokenRequestToken).
		Publish(ctx, []string{repository})
	if err != nil {
		return "", fmt.Errorf("publishing %s to %s/%s: %w", version, registry, repository, err)
	}

	var report strings.Builder
	fmt.Fprintf(&report, "%s/%s\n", registry, repository)
	for _, ref := range published {
		fmt.Fprintf(&report, "  %s\n", ref)
	}
	return report.String(), nil
}

// VersionScheme checks planVersion and matchesPublishOn against a table of
// cases: the version a build is stamped with and whether it publishes, for
// every shape of HEAD that matters.
//
// Every expected value is a literal, never one of this file's own values, so the
// check cannot move with the code it checks.
//
// +check
// +cache="session"
func (m *Dfcad) VersionScheme() error {
	const publishOn = `^refs/(heads/main|tags/v.+)$`

	versions := []struct {
		name      string
		refs      []string
		committed string
		version   string
		fails     bool
	}{
		{
			name:      "a branch build is the short sha and the commit time",
			refs:      []string{"refs/heads/main", "refs/remotes/origin/main"},
			committed: "2026-08-10T02:55:57Z",
			version:   "0b481ea-2026-08-10T02-55-57Z",
		},
		{
			name:      "a committer offset is made tag-safe rather than dropped",
			refs:      []string{"refs/heads/main"},
			committed: "2026-08-09T23:44:50-04:00",
			version:   "0b481ea-2026-08-09T23-44-50-04-00",
		},
		{
			name:      "a positive offset loses its plus sign to the tag charset",
			refs:      []string{"refs/heads/main"},
			committed: "2026-08-10T05:44:50+02:00",
			version:   "0b481ea-2026-08-10T05-44-50-02-00",
		},
		{
			name:      "a pull request's merge commit is versioned like a branch build",
			refs:      []string{"refs/remotes/pull/238/merge"},
			committed: "2026-09-26T04:13:25Z",
			version:   "0b481ea-2026-09-26T04-13-25Z",
		},
		{
			name:      "a commit with no refs at all is still versioned",
			committed: "2026-09-26T04:13:25Z",
			version:   "0b481ea-2026-09-26T04-13-25Z",
		},
		{
			name:      "a tag at head is the version, verbatim",
			refs:      []string{"refs/tags/v1.2.3", "refs/heads/main"},
			committed: "2026-09-26T04:13:25Z",
			version:   "v1.2.3",
		},
		{
			name:      "a prerelease tag is the version",
			refs:      []string{"refs/tags/v1.0.0-rc.1"},
			committed: "2026-09-26T04:13:25Z",
			version:   "v1.0.0-rc.1",
		},
		{
			name:      "the most recently created of two tags is the version",
			refs:      []string{"refs/tags/v1.2.4", "refs/tags/v1.2.3"},
			committed: "2026-09-26T04:13:25Z",
			version:   "v1.2.4",
		},
		{
			name:      "a tag carrying build metadata is refused rather than mangled",
			refs:      []string{"refs/tags/v1.2.3+build.5"},
			committed: "2026-09-26T04:13:25Z",
			fails:     true,
		},
		{
			name:      "a path-shaped tag is refused rather than mangled",
			refs:      []string{"refs/tags/release/v1.2.3"},
			committed: "2026-09-26T04:13:25Z",
			fails:     true,
		},
	}

	publishes := []struct {
		name      string
		refs      []string
		publishes bool
	}{
		{name: "main publishes", refs: []string{"refs/heads/main"}, publishes: true},
		{name: "main's remote-tracking ref counts as main", refs: []string{"refs/remotes/origin/main"}, publishes: true},
		{name: "a release tag publishes", refs: []string{"refs/tags/v1.2.3"}, publishes: true},
		{name: "a pull request's merge commit does not publish", refs: []string{"refs/remotes/pull/238/merge"}},
		{name: "another branch does not publish", refs: []string{"refs/heads/chore/backlog-reviewers-roster", "refs/remotes/origin/chore/backlog-reviewers-roster"}},
		{name: "a tag that is not a release does not publish", refs: []string{"refs/tags/nightly"}},
		{name: "a commit with no refs does not publish"},
	}

	var errs []error
	for _, c := range versions {
		got, err := planVersion(c.refs, "0b481ea", c.committed)
		switch {
		case c.fails && err == nil:
			errs = append(errs, fmt.Errorf("%s: versioned %q, want an error", c.name, got))
		case c.fails:
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", c.name, err))
		case got != c.version:
			errs = append(errs, fmt.Errorf("%s: version is %q, want %q", c.name, got, c.version))
		}
	}
	for _, c := range publishes {
		got, err := matchesPublishOn(c.refs, publishOn)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", c.name, err))
		case got != c.publishes:
			errs = append(errs, fmt.Errorf("%s: publishes is %t, want %t", c.name, got, c.publishes))
		}
	}
	return errors.Join(errs...)
}

// planVersion is the version of a build whose HEAD carries refs, is shortSHA,
// and was committed at committed, git's strict ISO 8601.
//
// refs are in the order headRefs reads them, most recently created first, so the
// first tag among them is the newest.
func planVersion(refs []string, shortSHA, committed string) (string, error) {
	for _, ref := range refs {
		tag, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/tags/")
		if !ok {
			continue
		}
		if strings.Contains(tag, "+") {
			return "", fmt.Errorf("tag %q carries build metadata, which cannot be spelled in an image tag: tag the release without it", tag)
		}
		if !ociTag.MatchString(tag) {
			return "", fmt.Errorf("tag %q cannot be spelled as an image tag, which may hold only [A-Za-z0-9_.-] and may not begin with . or -: tag the release as vMAJOR.MINOR.PATCH", tag)
		}
		return tag, nil
	}
	if shortSHA == "" || committed == "" {
		return "", errors.New("HEAD names no commit to version the build by")
	}
	return shortSHA + "-" + strings.NewReplacer(":", "-", "+", "-").Replace(committed), nil
}

// matchesPublishOn reports whether any of refs matches publishOn, a
// remote-tracking branch counting as the branch it tracks. A CI checkout of a
// push to main carries refs/remotes/origin/main as well as, or instead of,
// refs/heads/main, and the two are the same branch.
func matchesPublishOn(refs []string, publishOn string) (bool, error) {
	if publishOn == "" {
		return false, errors.New("publishOn is required: it is the regular expression over the refs at HEAD that decides whether a build publishes")
	}
	re, err := regexp.Compile(publishOn)
	if err != nil {
		return false, fmt.Errorf("publishOn %q is not a regular expression: %w", publishOn, err)
	}
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if branch, ok := strings.CutPrefix(ref, "refs/remotes/origin/"); ok {
			ref = "refs/heads/" + branch
		}
		if re.MatchString(ref) {
			return true, nil
		}
	}
	return false, nil
}

// headRefs is every ref pointing at HEAD, most recently created first.
func headRefs(ctx context.Context, source, gitDir *dagger.Directory) ([]string, error) {
	out, err := gitContainer(source, gitDir).
		WithExec([]string{"git", "for-each-ref", "--points-at", "HEAD", "--sort=-creatordate", "--format=%(refname)"}).
		Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the refs at HEAD: %w", err)
	}
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		if ref := strings.TrimSpace(line); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

// headCommit is HEAD's short SHA and its committer time in git's strict ISO
// 8601 — the commit's time rather than the build's, so every rebuild of one
// commit is the same version.
func headCommit(ctx context.Context, source, gitDir *dagger.Directory) (shortSHA, committed string, err error) {
	out, err := gitContainer(source, gitDir).
		WithExec([]string{"git", "show", "-s", "--format=%h%n%cI", "HEAD"}).
		Stdout(ctx)
	if err != nil {
		return "", "", fmt.Errorf("reading the commit at HEAD: %w", err)
	}
	shortSHA, committed, _ = strings.Cut(strings.TrimSpace(out), "\n")
	return strings.TrimSpace(shortSHA), strings.TrimSpace(committed), nil
}

// gitContainer is a container holding the source with its git metadata mounted
// back where git expects it. It is mounted rather than folded into the source,
// so that reading a ref costs none of the check stages their cache.
func gitContainer(source, gitDir *dagger.Directory) *dagger.Container {
	return dag.Go().Container(source).WithMountedDirectory("/src/.git", gitDir)
}
