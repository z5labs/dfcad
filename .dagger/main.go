// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

// dfcad's pipeline: the Z5Labs standard checks, the stamped binary, and the
// published image, all by way of the z5labs module pinned in dagger.json.
//
// The standard lives in github.com/z5labs/devex/daggerverse/z5labs, and this
// module adds nothing to it but the two decisions the standard leaves to its
// caller: which version a build is, and whether it is published. Both are read
// out of git at HEAD, in version.go, because that is what they were before the
// archetype stopped deciding them — see docs/publishing.md and
// docs/versioning.md.
//
// The dependency is pinned by commit rather than followed. An unpinned module is
// a pipeline that changes without a commit in this repository, and it broke
// every pull request here twice over — once when GoApp was replaced by the Go
// chain, and once when the module moved to an engine the pinned CLI could not
// load. The pin moves when somebody moves it, in a pull request that shows what
// the move did.
package main

import (
	"context"

	"dagger/dfcad/internal/dagger"
)

// binaryPath is where the archetype puts the application in every image it
// builds: /app/<binary>, the binary named for the last element of the go.mod
// module path.
const binaryPath = "/app/dfcad"

// defaultBinaryPlatform is the platform Binary builds for when it is not told:
// the one a CI runner and most workstations can run.
const defaultBinaryPlatform dagger.Platform = "linux/amd64"

type Dfcad struct {
	// Source is the repository without its git metadata. The four check stages
	// read none of it, and leaving it in would make every commit a cache miss
	// for all of them.
	Source *dagger.Directory
	// GitDir is the repository's .git, bound apart from Source and grafted back
	// on only where something reads it: the build, which stamps and annotates
	// from HEAD, and the version and publish decisions.
	GitDir *dagger.Directory
}

func New(
	// The repository to run against. Defaults to this one.
	//
	// +optional
	// +defaultPath="/"
	// +ignore=[".git", ".claude", ".dagger", "dagger.json", "/dfcad", "/gate-results", ".dfcad", "*.gfs"]
	source *dagger.Directory,
	// The repository's git metadata. Defaults to this one's.
	//
	// +optional
	// +defaultPath="/.git"
	// +ignore=["logs", "hooks", "index", "FETCH_HEAD", "ORIG_HEAD", "COMMIT_EDITMSG"]
	gitDir *dagger.Directory,
) *Dfcad {
	return &Dfcad{Source: source, GitDir: gitDir}
}

// Ci runs the Z5Labs standard checks: gofmt, go vet, golangci-lint against the
// module's bundled policy, and `go test ./...` with the race detector.
//
// It is the standard's own Ci and nothing beside it. A stage added here would
// be a second definition of the standard, and the two would drift.
//
// +check
// +cache="session"
func (m *Dfcad) Ci(ctx context.Context) error {
	return dag.Z5Labs().
		Go(m.Source).
		WithTest(true).
		Ci(ctx)
}

// Binary is the dfcad binary for one platform, stamped exactly as the published
// image's is.
//
// It is taken out of the image the archetype builds rather than compiled here,
// so the binary the model gate runs is the one the pipeline ships: a second way
// of producing it would be a second definition of the build, and the gate and
// the artifact could then disagree about what the tool does.
//
// +cache="session"
func (m *Dfcad) Binary(
	ctx context.Context,
	// The platform to build for. Defaults to linux/amd64.
	//
	// +optional
	platform dagger.Platform,
) (*dagger.File, error) {
	if platform == "" {
		platform = defaultBinaryPlatform
	}
	version, err := m.Version(ctx)
	if err != nil {
		return nil, err
	}
	return m.app(version, platform).Container(platform).File(binaryPath), nil
}

// Image is the dfcad image for one platform, exactly as Publish pushes it for
// that platform: the same archetype, the same version and the same stamp.
//
// It exists so that the model gate can be run through the image on a build
// which does not publish one. A consumer's gate runs the published image by
// digest, and the self-test proves that route on every pull request by loading
// this into the runner's image store — `dagger call image export-image
// --name=...` — and gating the broken models through it. Building the image a
// second way for the purpose would be a second definition of the artefact,
// which is what Binary is careful not to be either.
//
// +cache="session"
func (m *Dfcad) Image(
	ctx context.Context,
	// The platform to build for. Defaults to linux/amd64.
	//
	// +optional
	platform dagger.Platform,
) (*dagger.Container, error) {
	if platform == "" {
		platform = defaultBinaryPlatform
	}
	version, err := m.Version(ctx)
	if err != nil {
		return nil, err
	}
	return m.app(version, platform).Container(platform), nil
}

// app is the archetype's application at version, built for platforms, or for
// the pipeline's pair when none are given.
func (m *Dfcad) app(version string, platforms ...dagger.Platform) *dagger.Z5LabsApp {
	return dag.Z5Labs().
		Go(m.appSource()).
		App(version, dagger.Z5LabsGoChainAppOpts{
			Pkg:       "./cmd/dfcad",
			Platforms: platforms,
		})
}

// appSource is Source with its git metadata grafted back on, which the
// archetype requires of any tree it builds an application from: it stamps
// every binary with the commit and annotates every image with the commit, its
// committer time and the origin remote.
func (m *Dfcad) appSource() *dagger.Directory {
	return m.Source.WithDirectory(".git", m.GitDir)
}
