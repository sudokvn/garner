# Contributing to garner

Bug reports, ideas and pull requests are welcome. For a larger change, open an issue first, so we can agree on the approach before you build it.

## Ground rules

- Develop and test against the development stack below, never against a library you care about.
- Keep real release names out of the repository: in tests, comments and commit messages, use made-up ones such as `Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP`.
- Run `mise run check` before you push. GitHub checks every pull request and push to `main`; [Check your changes](#check-your-changes) describes how CI differs from the local check.
- Keep changes focused, with tests for the behaviour they change.
- Write the name garner in lowercase, also at the start of a sentence. Write the programs `garner` and `garnerd` in code font, so they read as commands.

## Run it locally

garner runs on your machine against a local development stack: SABnzbd, Prowlarr and Jellyfin in a Podman pod, so development never touches other services or libraries. You need [mise](https://mise.jdx.dev), [Podman](https://podman.io), a Usenet account, at least one Newznab indexer and a [TheTVDB API key](https://thetvdb.com/api-information): only release images have garner's own key built in.

Run the development tasks through `mise`, which uses the tool versions pinned in [mise.toml](mise.toml).

1. Copy `devstack.example.toml` to `devstack.toml`. Fill in your Usenet server and indexers. Set `TVDB_API_KEY` in the environment, for example in `mise.local.toml`:

   ```toml
   [env]
   TVDB_API_KEY = "..."
   ```

   Git ignores both files. A `devstack.toml` value written as `"${NAME}"` is read from that environment variable, so secrets can stay in a secret manager. `mise.local.toml` can also redefine the `dev` and `devstack` tasks to run through one.
2. Run `mise run devstack up`. It creates the stack in `~/garner-dev` and sets it up: SABnzbd with your Usenet server, Prowlarr with your indexers and Jellyfin with the TheTVDB plugin and the TV and anime libraries. It also writes `garner.toml` for the stack, with the monitor turned off, so the development server searches and downloads only what you ask for. It overwrites its own generated configuration but leaves a hand-written `garner.toml` alone. The first run downloads the images; later runs only bring the setup up to date.
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

Keep one-off files from testing, such as a copy of the dev data or a second `garner.toml` on another port, in `~/garner-dev/scratch`. The stack's containers do not mount it, but containers you start yourself can, since Podman shares your home folder. `mise run clean` deletes scratch and the build output in `bin/` and `dist/`, and keeps the stack and its data. Stop garner first.

`mise run devstack up` removes any Usenet server or indexer that is not in `devstack.toml`, so change them there rather than in SABnzbd or Prowlarr. Some providers and indexers limit how many places may use an account at once; give the stack an account, or an allowance, that nothing else needs.

## Check your changes

`mise run check` checks every file in the working folder that Git does not ignore, committed or not, with Git or jj. It needs Podman, but not the development stack. It runs formatting, vet, shellcheck, the link check, the generated code and notices checks and govulncheck. It runs the tests on your machine with the race detector, again on Linux in the Go image the container is built with, and builds both programs for Linux x86. [tools/check.sh](tools/check.sh) lists every step. If a file changed while it ran, it fails; run it again.

GitHub's [Check workflow](.github/workflows/check.yml) runs the same checks on every pull request and push to `main`. It runs the race detector on Linux instead of macOS, builds `garner` for every release platform and tests `install.sh` against the newest releases. Neither runs the maintainer's private real-case tests.

Other tasks:

- `mise run build` builds both programs without starting the server.
- `mise run links` checks only the links between the Markdown files.
- `mise run image` builds the container image `localhost/garner:dev` with Podman.

### Generated files

After changing `internal/store/*.sql` or the migrations, run `mise run generate`. It runs `sqlc` through `go generate ./...`. Never edit the generated code in `internal/store/query` by hand. Include the regenerated files with the SQL changes.

After changing a dependency, run `mise run notices`. The generator reads the Go modules compiled into the released binaries. Read its summary of modules, versions and licences. Include the generated `THIRD-PARTY-NOTICES.txt` with the dependency change. Never edit that file by hand.

A missing licence or one outside the allowlist in `tools/notices` stops generation. If a licence is missing, find its text before proceeding. If a licence is outside the allowlist, have the maintainer review whether garner may redistribute it before changing the allowlist or shipping the dependency. Do not bypass the check.

### Writing tests

Keep tests few. A test earns its place when it guards something users rely on or a property that keeps them safe: their files, the database, who may use the API, what garner decides about a release, and bugs or security problems that happened or could happen. Do not test internals that the tests of what users see already cover. There is no coverage target.

- Use the real parts: a real store in `t.TempDir()`, real files and `httptest` servers that answer as SABnzbd, Prowlarr, Jellyfin or TheTVDB do. Use only `testing`, with no mocking or assertion libraries.
- In `internal/importer`, only the guard (`internal/importer/guard`) may move, write or remove files. `TestOnlyTheGuardChangesFiles` fails when other code there calls `os.Rename`, `os.Remove` and the like.
- `internal/release`, `internal/match` and `internal/selection` check their own results and panic in tests when a check breaks. Their fuzz tests, and `FuzzParse` in `internal/profile`, look for input that breaks them: `go test ./internal/match -fuzz FuzzMatch -fuzztime 1m`.
- When fuzzing or the simulation finds a bug, add a focused test for it.

The maintainer also runs tests against real release names, which stay private. They run when `GARNER_MATCH_CASES`, `GARNER_SELECT_CASES` and `GARNER_RELEASE_CASES` point at the case files, and are skipped otherwise.

### The simulation

`internal/simulation` runs garner's server against simulated services and checks after every step that garner keeps its promises about the user's files. `go test ./...` runs it. A failure names the seed and the broken rule and ends with the command that replays it, such as:

```bash
go test ./internal/simulation -run TestSimulation -seed 1842 -steps 317 -v
```

When it finds a bug, add a focused test with the fix: after later changes, the same seed may take another path. The package comment in `internal/simulation/simulation_test.go` describes the promises, fresh seeds and the nightly run.

## Documentation

The [README](README.md) is the product overview. User guides live in `docs/`, the terminal interface's conventions in [internal/tui/README.md](internal/tui/README.md), runnable examples with their instructions in `examples/`, contributor rules here and the release process in [RELEASING.md](RELEASING.md). Link to a guide rather than repeating it, and keep configuration examples and [docs/profile.schema.json](docs/profile.schema.json) in line with what garner does.

Name garner and its programs as the [ground rules](#ground-rules) say. Use British spelling and plain, short sentences, with no em dashes and no Oxford commas. A link to a file or heading that does not exist fails the check; `mise run links` checks only the links.

## How the code is organised

| Folder | Holds |
| --- | --- |
| `cmd/garnerd` | The server: `serve`, plus `token`, `backup`, `status` and `profile import`, which reach the running server over its admin socket |
| `cmd/garner` | The terminal interface's entry point and `garner profile check`; its test keeps server packages out |
| `internal/tui` | The terminal interface, one file per screen; [its README](internal/tui/README.md) describes its conventions |
| `internal/client` | The HTTP client the terminal interface uses: it sends the token only to a verified server and follows the event stream |
| `internal/api` | The types the server and its clients share: shows, episodes, downloads, searches, progress, events and their states |
| `internal/connect`, `internal/token`, `internal/tlsident` | The connection code, API tokens and the server's own certificate with its pin, shared by server and client |
| `internal/profile` | The profile format: reading, checking and writing profiles, and the shipped ones in `shipped/`; no server code, so the terminal interface uses it too |
| `internal/server` | The HTTP API and the admin socket `garnerd` uses; it sets up the services below and runs their loops |
| `internal/config` | Reading `garner.toml`, and secrets from the environment |
| `internal/store` | The database: migrations, `sqlc` queries (generated into `internal/store/query`), tokens and backups; it also works out episode states and the monitor's schedule |
| `internal/catalog` | Show data from TheTVDB and TheXEM with reviewed corrections in `corrections.toml`, and searching TheTVDB for shows to add |
| `internal/release` | Reading release names: episodes, resolution, source and languages |
| `internal/match` | Matching a release to a show's episodes, including scene and absolute numbering |
| `internal/selection` | Applying a profile to a show: verdicts with their reasons and the ranking of releases |
| `internal/search` | Searching for an episode's or a season's releases through Prowlarr, judging them and storing the result |
| `internal/download` | Handing releases to SABnzbd, following them and replacing failed ones |
| `internal/monitor` | Searching due episodes and upgrades on its own, and a whole show on request |
| `internal/importer` | Checking finished downloads and moving them into the library; also taking over existing files, renaming them and deleting a show's files |
| `internal/importer/guard` | The only code that changes files and folders, and only those garner owns |
| `internal/inspect` | Reading video files with FFprobe and comparing them with their release |
| `internal/prowlarr`, `internal/sabnzbd`, `internal/jellyfin` | Clients for the services garner uses |
| `internal/httpx` | The HTTP clients for those services, and hiding credentials in URLs |
| `internal/buildinfo` | The version both programs report |
| `internal/simulation` | Tests only: the simulation, and the demo world `mise run demo` records |
| `internal/testprofiles` | Tests only: shipped profiles as defaults, and a helper that installs them into a store |
| `tools/devstack` | `mise run devstack`: the local development stack |
| `tools/notices` | `mise run notices`: generates `THIRD-PARTY-NOTICES.txt` |
| `tools/*.sh` | The scripts behind `mise run check`, `links`, `clean`, `demo` and `release`; `nightly-report.sh` files nightly failures as issues |

## Releases

Only the maintainer releases garner. [RELEASING.md](RELEASING.md) describes the versions, the channels and the release process.
