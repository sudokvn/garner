# Releasing garner

Only the maintainer releases garner. This guide describes the versions, the channels and how a commit of `main` becomes a release. Every change reaches `main` as [CONTRIBUTING.md](CONTRIBUTING.md) describes.

## Channels

garner has a stable and a dev channel; [Versions and channels](docs/install.md#versions-and-channels) describes them and how users install them. The dev channel gets a dev build of every push to `main` that passes the Check workflow, with no release. A candidate is a pre-release on GitHub and the step before a stable release; the Homebrew cask `garner@dev` follows the candidates. The maintainer's own server runs dev builds, and each candidate must run there before it becomes stable.

## Versions

Versions follow semantic versioning. Until 1.0, a new minor version, such as `v0.9.0`, brings new features, a change to the database or a setting that changes meaning or is no longer accepted. A patch version, such as `v0.9.1`, only fixes. Candidates count up per version: `v0.9.0-rc.1`, `v0.9.0-rc.2`. A dev build is no version of its own: it is named after the newest version tag in its history and the commits since, such as `v0.9.0-rc.2-dev.3`, and gets no tag.

## From change to release

1. A change lands on `main` after `mise run check`, as every change does. Once the Check workflow passes on it, it becomes a dev build, with no release; see [How a dev build is built](#how-a-dev-build-is-built). To try it, the maintainer upgrades their server to it by hand.
2. When `main` holds something worth releasing, release it as a candidate: `mise run release v0.9.0-rc.1`. It goes out as a pre-release, and the maintainer upgrades their server and terminal interface to it. Fixes found meanwhile go into the next candidate.
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

- the source archive, `garner-VERSION-source.tar.gz`, with `tools/source.sh`: the tag's files with the Go modules, the FFmpeg release FFprobe is built from, and the source of Turso's native library with its Rust crates. [Build garner from its source archive](docs/source.md#build-garner-from-its-source-archive) describes it.
- the server image from the source archive's `garner/` folder, with garner's TheTVDB key from the repository secret `TVDB_API_KEY` built in; the workflow fails without it. The image names its source archive in its description and to the terminal interface. The workflow keeps the image on the runner until GoReleaser has published the release with the source archive, then pushes it as `ghcr.io/sudokvn/garner:VERSION` and names its digest in the release. The newest stable version also gets the tag `latest`, which the Compose example in `examples/compose` pulls. The tag `dev` belongs to the dev builds. Intermediate layers stay in GitHub Actions' cache rather than the public registry. Caches are scoped to a tag: a rerun may reuse them, but each new version rebuilds FFprobe.
- with GoReleaser (`.goreleaser.yaml`), the terminal interface for macOS and Linux, on ARM and x86: one `garner-VERSION-OS-ARCH.tar.gz` per platform with `garner`, `LICENSE`, `NOTICE` and `THIRD-PARTY-NOTICES.txt`. It publishes them with SHA-256 sums and the source archive in the GitHub release, with the image's tag and the notes. The binaries name the source archive, as the image does.

`tools/release-notes.sh` writes the notes: the commits since the last published stable release that change garner itself, then an Upgrading section when the version changes the database or the tag carries a note. Commits that only change the documentation, the tests, CI or the scripts in `tools/` are left out; the build files, such as `.goreleaser.yaml`, count.

GoReleaser signs the macOS binaries with garner's Developer ID certificate and has Apple notarise them, so macOS opens them. The certificate and its password come from the repository secrets `MACOS_SIGN_P12` and `MACOS_SIGN_PASSWORD`, the App Store Connect key for notarising from `MACOS_NOTARY_KEY`, `MACOS_NOTARY_KEY_ID` and `MACOS_NOTARY_ISSUER_ID`; the workflow fails without them. The maintainer keeps copies in Infisical (`garner`, `dev`, `/macos-signing`). The certificate expires on 27 November 2027; binaries signed before then stay valid, and later releases need a new certificate in both places.

GoReleaser also updates the Homebrew casks in [`sudokvn/homebrew-tap`](https://github.com/sudokvn/homebrew-tap), `garner` for a stable release and `garner@dev` for every newest version, candidates included, with that repository's deploy key from the secret `HOMEBREW_TAP_KEY`; Infisical keeps a base64 copy in `/homebrew-tap`. The [install guide](docs/install.md#versions-and-channels) describes the installer and Homebrew channels.

To run GoReleaser's part locally into `dist/` without publishing anything, build a source archive first:

```bash
bash tools/source.sh v0.0.0 /tmp/garner-source
SOURCE_ARCHIVE=/tmp/garner-source/garner-v0.0.0-source.tar.gz goreleaser release --snapshot --clean
```

A snapshot's binaries name no source archive. The workflow keeps the twenty newest release images: those tagged only with versions, so neither dev images nor the image tagged `latest` count or are removed.

## How a dev build is built

GitHub's dev workflow (`.github/workflows/dev.yml`) runs when the Check workflow passes on a push to `main`. It builds the commit Check tested; a newer build cancels an older one still running. To start one by hand: `gh workflow run dev.yml --ref main`.

The build's version is the newest version tag merged into the commit, then `-dev.` and the number of commits since that tag (`dev_version` in `tools/versions.sh`): `v0.11.0-rc.3-dev.2`, or `v0.11.0-dev.0` for the commit of the stable release `v0.11.0`. It builds what a release builds, from the same files:

- the source archive, with `tools/source.sh`.
- the server image from it, with the TheTVDB key and the version in `garnerd` and in the image's version label. It pushes the image as `ghcr.io/sudokvn/garner:VERSION` and moves the tag `dev` to it. The layers come from an Actions cache that all dev builds on `main` share, so FFprobe is rebuilt only when it changes.
- with GoReleaser as a snapshot, the terminal interface's Linux archives for ARM and x86. It does not sign anything or update a Homebrew cask.

The pre-release `dev` holds only the newest dev build: its Linux archives, `SHA256SUMS` and the source archive. Each build replaces them and moves the tag `dev` to its commit; the ruleset protects only version tags. The release's title is the version. Its body names the image as releases do, `` Server image: `ghcr.io/sudokvn/garner:VERSION@sha256:…` ``, and the commit. `homelab upgrade garner dev` reads both. The binaries and the image name the source archive in the release `dev`, which the next dev build replaces.

The workflow keeps twenty dev images: the one tagged `dev` and the nineteen newest before it, counting only images tagged with nothing but dev versions. It never removes a release's image. Removing images needs the repository to have the Admin role under the package's Manage Actions access on GitHub. Without it, both workflows fail to remove images but still publish.

### Third-party notices and Turso's library

Every release ships `THIRD-PARTY-NOTICES.txt`: the licence texts of the Go modules compiled into `garner` and `garnerd`, then those of Turso's native library, which `garnerd` embeds. `mise run notices` regenerates it, as [Generated files](CONTRIBUTING.md#generated-files) describes. The native part comes from the reviewed snapshot `tools/turso/NOTICES.txt`. That snapshot records the packages, licence texts and provenance of supplemental texts for one native binary. Ordinary garner releases reuse it without downloading or rescanning Rust's dependency graph.

Turso's Go module `github.com/tursodatabase/turso-go-platform-libs` holds the native library compiled, not its source. `tools/turso/turso.env` pins where it comes from: the module's commit, the Turso commit it was built from, the library's checksum, the Rust version and the build settings. `tools/source.sh` fetches that source into the source archive, and `mise run notices` fails while go.mod and the pins disagree. When go.mod moves to another `tursogo` version:

1. Find the platform module's commit that adds the libraries for the new version; its message names the Turso tag. Find the commit that tag points to in Turso's repository.
2. Update the source and build pins in `tools/turso/turso.env`, including the checksum of the embedded library. Check that Turso's `rust-toolchain.toml` still asks for the pinned Rust version; otherwise update `RUST_VERSION`, `RUST_COMMIT` and `RUST_IMAGE`.
3. Review `tools/turso/NOTICES.txt` against the new library's dependencies, bundled code and Rust runtime. In the unpacked Turso source, use `cargo tree --locked --package turso_sync_sdk_kit --target x86_64-unknown-linux-musl --edges normal,no-proc-macro` to identify the runtime crates. Read their licence files in `vendor/<crate>-<version>/`, Turso's `LICENSE.md`, `NOTICE.md` and `licenses/core/`, and Rust's licence files and `library/Cargo.lock` at `RUST_COMMIT`. Include notices from generated code where applicable, not just linked crates. Preserve applicable notices and add those of new components. Record the new build identity and library checksum only after that review; relabelling the old inventory is not a review. New licences need the maintainer's approval. Unicode-3.0 and Apache-2.0 WITH LLVM-exception were approved for the existing inventory, alongside the permissive licences in `tools/notices`.
4. Run `mise run notices`. It refuses a mismatch between the module versions, source pins, actual native binary and reviewed inventory.
5. Build a source archive. Check that it contains the pinned source, dependency sources and build scripts for the library being distributed.

[Build Turso's native library](docs/source.md#build-tursos-native-library) provides an additional check when investigating the source mapping or build inputs. A local rebuild is not a release gate, and byte-identical output is not required. The archive must still contain the actual corresponding source; an unexplained source-version mismatch must be resolved before releasing.

## If a release fails

Version tags cannot be deleted or moved. A failed workflow leaves its version taken, but it does not undo what was published:

- The workflow pushes the version's server image only once the GitHub release, with its source archive, is published. Without a published release, neither the image of that version nor its intermediate layers are published on `ghcr.io`.
- Before GoReleaser starts publishing, no GitHub release or Homebrew cask has been published for that version by the run. The image channel tags have not moved.
- During publishing, GoReleaser may leave a draft release with some assets uploaded. It publishes the GitHub release only with all its assets, then updates the Homebrew casks, so a cask failure can leave a published release and some casks already updated. The workflow still pushes that release's image and names its digest in the release.
- If pushing the image fails after the release was published, the release names an image tag that does not exist.
- If GoReleaser or the push fails, the workflow does not run the step that moves the image channel tags. If that later step itself fails, a channel tag may already have moved.

The steps after GoReleaser builds depend on outside services: the Homebrew tap's deploy key, `ghcr.io` and GitHub itself. They rarely fail on their own.

After a failure, read the workflow run and check what was published. Fix the cause, then release the next version:

- A candidate that was not published: the next candidate number.
- A stable release that was not published: a later patch of the same minor version, which may reuse the newest candidate under the rules above.
- A release that was published, but whose Homebrew casks or image tags were not updated: the next version, as usual. Its release updates them.
