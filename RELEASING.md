# Releasing harmonik

Operational checklist for cutting a harmonik release. Releases ship prebuilt
binaries for `linux`/`darwin` × `amd64`/`arm64` plus a `checksums.txt`, built by
[GoReleaser](https://goreleaser.com/) from `.goreleaser.yaml`.

## Prerequisites

- **GoReleaser v2** installed:
  ```bash
  go install github.com/goreleaser/goreleaser/v2@latest
  # binary lands at $(go env GOPATH)/bin/goreleaser
  ```
- **GitHub auth** via the `gh` CLI, with the `repo` and `workflow` scopes:
  ```bash
  gh auth login          # or: gh auth refresh -h github.com -s repo,workflow
  gh auth status         # confirm the scopes above are present
  ```
  The `workflow` scope is what lets you push tags that trigger the release
  Action (path (a) below). Without it, use the local path (b).

## 1. Vet gate

Run the preflight gate from a clean `main`:

```bash
scripts/release-preflight.sh
```

It asserts you are on a clean `main` in sync with `origin/main`, runs
`make build` + `make check`, validates `.goreleaser.yaml`, cross-compiles a
4-platform snapshot, and confirms `dist/` holds all four binaries. On success it
prints the validated SHA and `PREFLIGHT GREEN — safe to tag <SHA>`.

**In-practice validation:** the strongest signal is the live daemon processing
real beads green. Before releasing, let the daemon run a while on real work and
confirm beads land green, then cut the release **off that exact commit**. The
preflight gate is the mechanical floor; a green daemon on the same SHA is the
real proof.

## 2. Cut the release

Tag with [semver](https://semver.org/), then pick a path.

### Path (a) — CI (preferred, once the `workflow` scope is granted)

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which runs
GoReleaser and publishes the GitHub release:

```bash
git tag -a v0.1.0 -m "harmonik v0.1.0"
git push origin v0.1.0
```

Watch the run: `gh run watch` (or the Actions tab). The release appears on the
[Releases page](https://github.com/gregberns/harmonik/releases) when it finishes.

### Path (b) — Local (used for v0.1.0, no `workflow` scope)

If your token lacks the `workflow` scope, cut the release from your machine
after tagging:

```bash
git tag -a v0.1.0 -m "harmonik v0.1.0"
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean
```

GoReleaser builds, archives, generates checksums, and uploads the GitHub release
directly using the token. (Path (b) was used for the initial v0.1.0 release.)

## 3. Versioning policy

Semantic versioning. harmonik is **pre-1.0 (v0.x)** while the daemon and queue
APIs are still moving — expect breaking changes between minor versions until
those surfaces stabilize. Use `-rc`/`-beta`/`-alpha` suffixes for prereleases;
`.goreleaser.yaml` has `prerelease: auto`, so such tags publish as GitHub
prereleases automatically.

## 4. Verify the release

Download a published asset and confirm the stamped commit matches the tag:

```bash
gh release download v0.1.0 --repo gregberns/harmonik --pattern 'harmonik_*_darwin_arm64.tar.gz'
tar xzf harmonik_*_darwin_arm64.tar.gz
./harmonik version
# Expect: harmonik v0.1.0 (commit <sha>, built <date>) darwin/arm64
# Confirm the printed commit matches the SHA the tag points at:
git rev-parse v0.1.0^{commit}
```

The commit printed by `harmonik version` must equal the tagged SHA. If it prints
`dev`/`unknown`, the binary was not built through GoReleaser (e.g. a plain
`go install`) — re-fetch the release asset.
