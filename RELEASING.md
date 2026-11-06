# Releasing garner

Only the maintainer releases garner. This guide describes the versions, the channels and how a commit of `main` becomes a release. Every change reaches `main` as [CONTRIBUTING.md](CONTRIBUTING.md) describes.

## Channels

garner has a stable and a dev channel; [Versions and channels](docs/install.md#versions-and-channels) describes them and how users install them. A candidate is a pre-release on GitHub. The maintainer's own server and terminal interface run the dev channel, where each candidate must run before it becomes stable.

## Versions

Versions follow semantic versioning. Until 1.0, a new minor version, such as `v0.9.0`, brings new features, a change to the database or a setting that changes meaning or is no longer accepted. A patch version, such as `v0.9.1`, only fixes. Candidates count up per version: `v0.9.0-rc.1`, `v0.9.0-rc.2`.

## From change to release

1. A change lands on `main` after `mise run check`, as every change does.
2. When `main` holds something worth releasing, release it as a candidate: `mise run release v0.9.0-rc.1`. It goes to the dev channel, and the maintainer upgrades their server and terminal interface to it. Fixes found meanwhile go into the next candidate.
3. Run the nightly on the newest candidate: `gh workflow run nightly.yml --ref v0.9.0-rc.1`. The scheduled nightly tests `main`, which may have moved on.
4. Once the newest candidate proved itself, release it as stable: `mise run release v0.9.0` releases the commit of the newest candidate.

`mise run release` checks release eligibility, not the code. Before a stable release, the script enforces these rules:

- A published candidate exists after the last published stable release.
- The release uses the newest candidate's commit.
- The version matches that candidate's version. If the stable tag already exists but its release was not published on GitHub, a later patch of the same major and minor version may reuse the candidate. The new version must be newer than every existing version tag.
- The nightly passed on that commit.
- For a new minor version, 7 days have passed since the first candidate was published and 3 days since the newest. For a patch candidate, 1 day has passed since the newest was published. Reusing a candidate after a failed stable release keeps that candidate's waiting times.

`--hotfix` skips only the waiting times. It does not skip the candidate or nightly requirements.

The maintainer must judge whether a candidate proved itself. No script checks these requirements:

- The candidate actually ran on the maintainer's server before becoming stable.
- Stable releases go out at most once a week, except hotfixes.
- A hotfix is justified by data loss, a security problem or imports broken for everyone. Its candidate still runs on the maintainer's server for a few hours first.

There is no fixed release day.

## How a release is built

`mise run release VERSION [revision] [--note TEXT] [--hotfix]` tags a commit of `main` and pushes the tag. A candidate defaults to the newest commit of `main`; stable defaults to the newest candidate's commit. The commit must be on `main` on GitHub. A candidate must bring a commit not already included in the newest published release. A stable release must bring a commit not already included in the newest published stable release. Before it tags, the task shows the release notes the workflow will write. Use `--note` for what users must do or know before upgrading, such as a setting that is no longer accepted. GitHub's release workflow (`.github/workflows/release.yml`) then builds:

- the server image, `ghcr.io/sudokvn/garner:VERSION`, with garner's TheTVDB key from the repository secret `TVDB_API_KEY` built in; the workflow fails without it. Once the release is out, the newest version also gets the tag `dev`, and the newest stable one `latest`, which the Compose example in `examples/compose` pulls. The workflow moves these channel tags only after GoReleaser succeeds. Its build cache lives on `ghcr.io` too, so FFprobe is compiled only when the Dockerfile's FFmpeg changes.
- with GoReleaser (`.goreleaser.yaml`), the terminal interface for macOS and Linux, on ARM and x86, with SHA-256 sums, and the GitHub release with the image reference, `THIRD-PARTY-NOTICES.txt` and its notes.

`tools/release-notes.sh` writes the notes: the commits since the last published stable release that change garner itself, then an Upgrading section when the version changes the database or the tag carries a note. Commits that only change the documentation, the tests, CI or the scripts in `tools/` are left out; the build files, such as `.goreleaser.yaml`, count.

GoReleaser signs the macOS binaries with garner's Developer ID certificate and has Apple notarise them, so macOS opens them. The certificate and its password come from the repository secrets `MACOS_SIGN_P12` and `MACOS_SIGN_PASSWORD`, the App Store Connect key for notarising from `MACOS_NOTARY_KEY`, `MACOS_NOTARY_KEY_ID` and `MACOS_NOTARY_ISSUER_ID`; the workflow fails without them. The maintainer keeps copies in Infisical (`garner`, `dev`, `/macos-signing`). The certificate expires on 27 November 2027; binaries signed before then stay valid, and later releases need a new certificate in both places.

GoReleaser also updates the Homebrew casks in [`sudokvn/homebrew-tap`](https://github.com/sudokvn/homebrew-tap), `garner` for a stable release and `garner@dev` for every newest version, with that repository's deploy key from the secret `HOMEBREW_TAP_KEY`; Infisical keeps a base64 copy in `/homebrew-tap`. The [install guide](docs/install.md#versions-and-channels) describes the installer and Homebrew channels.

`goreleaser release --snapshot --clean` runs GoReleaser's part locally into `dist/` without publishing anything. The workflow keeps the twenty newest image versions, build cache included.

## If a release fails

Version tags cannot be deleted or moved. A failed workflow leaves its version taken, but it does not undo what was published:

- The version's server image may already exist on `ghcr.io`, because the workflow pushes it before running GoReleaser.
- Before GoReleaser starts publishing, no GitHub release or Homebrew cask has been published for that version by the run. The image channel tags have not moved.
- During publishing, GoReleaser may leave a draft release with some assets uploaded. It publishes the GitHub release before updating the Homebrew casks, so a cask failure can leave a published release and some casks already updated.
- If GoReleaser fails, the workflow does not run the step that moves the image channel tags. If that later step itself fails, a channel tag may already have moved.

The steps after GoReleaser builds depend on outside services: the Homebrew tap's deploy key, `ghcr.io` and GitHub itself. They rarely fail on their own.

After a failure, read the workflow run and check what was published. Fix the cause, then release the next version:

- A candidate that was not published: the next candidate number.
- A stable release that was not published: a later patch of the same minor version, which may reuse the newest candidate under the rules above.
- A release that was published, but whose Homebrew casks or image channel tags were not updated: the next version, as usual. Its release updates them.
