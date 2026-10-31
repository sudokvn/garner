# Contributing to garner

Bug reports, ideas and pull requests are welcome. For a larger change, open an issue first, so we can agree on the approach before you build it.

## Ground rules

- Develop and test against the development stack below, never against a library you care about.
- Keep real release names out of the repository: in tests, comments and commit messages, use made-up ones such as `Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP`.
- Run `mise run check` before you push. Every pull request runs the same checks on GitHub, without the macOS tests.
- Keep changes focused, with tests for the behaviour they change.
- Write the name garner in lowercase, also at the start of a sentence. Write the programs `garner` and `garnerd` in code font, so they read as commands.

## Run it locally

garner runs on your machine against a local development stack: SABnzbd, Prowlarr and Jellyfin in a Podman pod, so development never touches other services or libraries. You need [mise](https://mise.jdx.dev), [Podman](https://podman.io), a Usenet account, at least one Newznab indexer and a [TheTVDB API key](https://thetvdb.com/api-information): only release images have garner's own key built in.

1. Copy `devstack.example.toml` to `devstack.toml` and fill in your Usenet server and indexers. Set `TVDB_API_KEY` in the environment, for example in `mise.local.toml`:

   ```toml
   [env]
   TVDB_API_KEY = "..."
   ```

   Git ignores both files. A `devstack.toml` value written as `"${NAME}"` is read from that environment variable, so secrets can stay in a secret manager. `mise.local.toml` can also redefine the `dev` and `devstack` tasks to run through one.
2. Run `mise run devstack up`. It creates the stack in `~/garner-dev` and sets it up: SABnzbd with your Usenet server, Prowlarr with your indexers, and Jellyfin with the TheTVDB plugin and the TV and anime libraries. It also writes `garner.toml` for the stack, with the monitor turned off, so the development server searches and downloads only what you ask for. The first run downloads the images; later runs only bring the setup up to date.
3. Run `mise run dev`. The task builds the server, `bin/garnerd`, and starts it against the stack. [Air](https://github.com/air-verse/air) rebuilds and restarts it whenever the code changes; the terminal interface reconnects on its own.
4. In a second terminal, run `./bin/garnerd token create <name>` to create a token.
5. Run `mise run tui` to build the terminal interface, `bin/garner`, and open it. Paste the connection code from step 4. The terminal interface stores the connection in `~/.config/garner/connection`.

| Service | Address | Sign-in |
| --- | --- | --- |
| SABnzbd | <http://127.0.0.1:8080> | None |
| Prowlarr | <http://127.0.0.1:9696> | Set up on first visit |
| Jellyfin | <http://127.0.0.1:8096> | User `garner-dev`, password in `~/garner-dev/secrets/jellyfin_admin_password` |

The stack listens on `127.0.0.1` only. Downloads, the library, the settings and garner's development database all live in `~/garner-dev`. The stack's own API keys are in `~/garner-dev/secrets`.

- `mise run devstack status` shows the containers.
- `mise run devstack down` stops them and keeps the data.
- `mise run devstack reset` deletes everything in `~/garner-dev` and builds the stack again. Stop garner first.

Keep one-off files from testing, such as a copy of the dev data or a second `garner.toml` on another port, in `~/garner-dev/scratch`; Podman sees that folder too. `mise run clean` deletes it, along with the build output in `bin/` and `dist/`, and keeps the stack and its data. Stop garner first.

`mise run devstack up` removes any Usenet server or indexer that is not in `devstack.toml`, so change them there rather than in SABnzbd or Prowlarr. Some providers and indexers limit how many places may use an account at once; give the stack an account, or an allowance, that nothing else needs.

## Check your changes

`mise run check` checks the working copy:

- formatting, `go vet`, the generated database code and known vulnerabilities
- the tests on macOS with the race detector, and again on Linux in the Go image the container is built with
- a build for Linux on x86

Other tasks:

- `mise run build` builds both programs without starting the server.
- `mise run generate` regenerates the database code after changes to `internal/store/*.sql` or the migrations.
- `mise run image` builds the container image `localhost/garner:dev` with Podman.

### Writing tests

A test guards behaviour someone relies on: the user's files, the database, who may use the API and what garner decides about a release. There is no coverage target.

- Use the real parts: a real store in `t.TempDir()`, real files in temporary folders and `httptest` servers that answer as SABnzbd, Prowlarr, Jellyfin or TheTVDB do. garner uses no mocking or assertion libraries, only `testing`, with failure messages that say what went wrong.
- Use made-up show and release names, as the ground rules say.
- When you fix a bug, write its test first and watch it fail.
- In `internal/importer`, only the guard (`internal/importer/guard`) may move, write or remove files. `TestOnlyTheGuardChangesFiles` fails when other code there calls `os.Rename`, `os.Remove` and the like.
- `internal/release`, `internal/match` and `internal/selection` check their own results. In tests, a broken check panics, so every test that reaches them checks them too. Their fuzz tests look for names that break them, for example `go test ./internal/match -fuzz FuzzMatch -fuzztime 1m`.
- The race detector needs cgo, so `mise run check` uses it for the macOS tests only. GitHub runs the tests with it on Linux.

The maintainer also runs tests against real release names, which stay private. When `GARNER_MATCH_CASES`, `GARNER_SELECT_CASES` and `GARNER_RELEASE_CASES` point at such case files, `go test ./...` includes them; without them, those tests are skipped. The pull request template reminds maintainers to run them before merging a change to `internal/release`, `internal/match` or `internal/selection`.

### The simulation

`internal/simulation` runs garner's server against a simulated SABnzbd, Prowlarr, Jellyfin and FFprobe, on a temporary share and a simulated clock. The share holds a library with files from before garner, the user's own files and an older Jellyfin library; the downloads or the older library may sit on another disk. A seed drives every event: what the user does, how the services answer and fail, when a folder is unplugged or refuses writes, how the catalogue changes, when time jumps and when garner restarts. The monitor runs too, with upgrades on. Inside a step, right before garner changes a file, the seed may end garner as a power cut would, and start it again from its database, or run another step first, as another request or one of garner's other loops may. After every step, the test checks garner's promises about files:

- Files no one gave garner, such as the user's own files and Jellyfin's metadata, stay as they are.
- A video leaves the library only when a file as good or better replaced it, or a worse one the user chose over it, or when the user deleted its show with its files.
- garner changes nothing outside the library and SABnzbd's folders, and removes a download's folder only when it is done with the download.
- Every file garner records exists.
- Downloads and imports change state only along the allowed transitions.

After a calm period at the end, every download and every import must have ended, every wanted episode that aired must have a file unless it waits for the user, garner must have removed SABnzbd's folders of the downloads it imported, and a video that was an episode's file must be gone once no episode records it, as after an upgrade.

`go test ./...` runs a fixed list of seeds, each twice: the second run must tell the same story. A failure names the seed, the step, the broken rule and the last steps, and ends with a command that replays it:

```bash
go test ./internal/simulation -run TestSimulation -seed 1842 -steps 317 -v
```

A failure also says whose mistake it is: garner broke a rule, or the simulation is out of step with garner, when its own notes of what it told garner, such as which shows the user added, differ from garner's database. The simulation checks those notes before every rule.

With `-v`, the replay prints every step. To explore, run `-seeds 1000` for a thousand fresh seeds instead of the fixed ones, or `-seeds-for 10m` for fresh seeds during ten minutes. Both stop at the first seed that fails.

Every night, GitHub's Nightly workflow (`.github/workflows/nightly.yml`) runs fresh seeds for 50 minutes and each fuzz test for 15 minutes. Fuzzing goes on from the inputs earlier nights found, which GitHub's cache keeps; the run's summary says how many there are. A failure opens an issue labelled `nightly` with the commit and how to replay it, or adds a comment to the one still open. Close it with the fix.

A failure is a bug in garner or in the simulation. When you fix a bug it found, also add a focused test for it: after later changes, the same seed may take another path.

The fixed seeds were chosen so that, together, every event changes something, a crash and another step meet every step that changes files, and they catch bugs built into garner on purpose. A change to the simulated world sends every seed down another path; the test then fails when an event or a crash point no longer shows up in any fixed seed. The comment on `fixedSeeds` says how to choose them again and lists the bugs to check by hand.

## How the code is organised

| Folder | Holds |
| --- | --- |
| `cmd/garnerd` | The server's commands: `serve`, `token`, `backup`, `status`, `profile` |
| `cmd/garner` | The terminal interface's entry point and `garner profile check` |
| `internal/release` | Reading release names: episodes, resolution, source, languages |
| `internal/match` | Matching a release to a show's episodes, including scene and absolute numbering |
| `internal/profile` | The profile format: reading, checking and writing profiles, and the shipped ones; no server code, so the terminal interface uses it too |
| `internal/selection` | A profile applied to a show, verdicts with their reasons, and ranking |
| `internal/testprofiles` | For tests only: the rules of the profiles from before profiles moved into the database |
| `internal/search`, `internal/download`, `internal/importer`, `internal/monitor` | Searching through Prowlarr, downloading with SABnzbd, checking and importing files, and the monitor |
| `internal/catalog` | Show data from TheTVDB and TheXEM, with reviewed corrections in `corrections.toml` |
| `internal/store` | The database, its migrations and the `sqlc` queries |
| `internal/server`, `internal/api`, `internal/client` | The HTTP API, its JSON types and the client the terminal interface uses |
| `internal/tui` | The terminal interface; [docs/tui.md](docs/tui.md) describes its conventions |

## Releases

### Channels

garner has two channels:

| Channel | Versions | Server image | Terminal interface |
| --- | --- | --- | --- |
| stable | stable releases, such as `v0.9.0` | `latest` | `install.sh`, the Homebrew cask `garner` |
| dev | release candidates, such as `v0.9.0-rc.1`, and every stable release | `dev` | `install.sh --channel dev`, the Homebrew cask `garner@dev` |

A candidate is a pre-release on GitHub. The maintainer's own server and terminal interface run the dev channel, so every stable release has run there first.

### Versions

Versions follow semantic versioning. Until 1.0, a new minor version, such as `v0.9.0`, brings new features, a change to the database or a setting that changes meaning or is no longer accepted. A patch version, such as `v0.9.1`, only fixes. Candidates count up per version: `v0.9.0-rc.1`, `v0.9.0-rc.2`.

### From change to release

1. A change lands on `main` after `mise run check`, as every change does.
2. When `main` holds something worth releasing, release it as a candidate: `mise run release v0.9.0-rc.1`. It goes to the dev channel, and the maintainer upgrades their server and terminal interface to it. Fixes found meanwhile go into the next candidate.
3. Run the nightly on the newest candidate: `gh workflow run nightly.yml --ref v0.9.0-rc.1`. The scheduled nightly tests `main`, which may have moved on.
4. Once the newest candidate proved itself, release it as stable: `mise run release v0.9.0` releases the commit of the newest candidate.

`mise run release` refuses a stable release until:

- a candidate of it exists, and the release is the commit of the newest candidate: what goes out is what ran. Only when a stable release failed, so its version is taken, may the next patch version release the same candidate;
- the nightly passed on that commit;
- the candidate ran long enough: for a new minor version, 7 days since its first candidate and 3 days since the newest; for a patch version, 1 day.

`--hotfix` skips the waiting, not the candidate or the nightly. Use it only for data loss, a security problem or imports broken for everyone; the candidate still runs on the maintainer's server for a few hours first.

There is no fixed release day. Fixes collect: at most one stable release a week, hotfixes aside.

### How a release is built

`mise run release VERSION [revision] [--note TEXT] [--hotfix]` tags a commit of `main`, the newest by default, and pushes the tag. The commit must be on `main` on GitHub and newer than the last published release. Before it tags, the task shows the release notes the workflow will write. Use `--note` for what users must do or know before upgrading, such as a setting that is no longer accepted. GitHub's release workflow (`.github/workflows/release.yml`) then builds:

- the server image, `ghcr.io/trucke/garner:VERSION`, with garner's TheTVDB key from the repository secret `TVDB_API_KEY` built in; the workflow fails without it. Once the release is out, the newest version also gets the tag `dev`, and the newest stable one `latest`, which the Compose example in `examples/compose` pulls; a failed release leaves both where they were. Its build cache lives on `ghcr.io` too, so FFprobe is compiled only when the Dockerfile's FFmpeg changes.
- with GoReleaser (`.goreleaser.yaml`), the terminal interface for macOS and Linux, on ARM and x86, with SHA-256 sums, and the GitHub release with the image reference and its notes from `tools/release-notes.sh`: the commits since the last published stable release that change garner itself, leaving out those that only change the documentation, the tests, CI or the scripts in `tools/` (the build files, such as `.goreleaser.yaml`, count), then an Upgrading section when the version changes the database or the tag carries a note. The release also carries `THIRD-PARTY-NOTICES.txt`, the licenses of the Go modules the binaries are built with; run `mise run notices` to regenerate it when a dependency changed.

GoReleaser signs the macOS binaries with garner's Developer ID certificate and has Apple notarize them, so macOS opens them. The certificate and its password come from the repository secrets `MACOS_SIGN_P12` and `MACOS_SIGN_PASSWORD`, the App Store Connect key for notarizing from `MACOS_NOTARY_KEY`, `MACOS_NOTARY_KEY_ID` and `MACOS_NOTARY_ISSUER_ID`; the workflow fails without them. The maintainer keeps copies in Infisical (`garner`, `dev`, `/macos-signing`). The certificate expires on 27 November 2027; binaries signed before then stay valid, and later releases need a new certificate in both places.

GoReleaser also updates the Homebrew casks in [`trucke/homebrew-tap`](https://github.com/trucke/homebrew-tap), `garner` for a stable release and `garner@dev` for every newest version, with that repository's deploy key from the secret `HOMEBREW_TAP_KEY`; Infisical keeps a base64 copy in `/homebrew-tap`. `install.sh` in the repository root installs the newest release of a channel; GitHub's check runs it for both on every push.

`goreleaser release --snapshot --clean` runs GoReleaser's part locally into `dist/` without publishing anything. The workflow keeps the twenty newest image versions, build cache included.

Version tags cannot be deleted or moved. When the workflow fails, its version stays taken and nothing is published: fix the cause and release the next version, the next candidate or, from the same candidate, the next patch version.
