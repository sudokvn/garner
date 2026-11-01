# Agent rules

Follow [CONTRIBUTING.md](CONTRIBUTING.md); its [ground rules](CONTRIBUTING.md#ground-rules) apply to agents too. This file maps tasks to the guides that hold the rules and workflows.

## Restrictions and approval gates

- Develop and test only against the [local development stack](CONTRIBUTING.md#run-it-locally), never other service instances or a real library.
- Never print secret values, including tokens and connection codes; list secrets by name only.
- In zsh, never write `2>&1 >/dev/null` around a pipe: MULTIOS sends output to both.
- Keep real release names out of the repository, including tests, comments and commit messages.
- Report vulnerabilities privately, never in a public issue; see [SECURITY.md](SECURITY.md#report-a-vulnerability).
- Push only after the user confirms.
- Release only after the user confirms.
- Release as a candidate unless the user asks for stable.
- Use `--hotfix` only when the user asks for it.
- `mise run release` releases only what is already pushed and checks release eligibility, not the code.

## Read for the task

Read the linked guides before starting the task.

| When | Read |
| --- | --- |
| Understand garner or find the code | [README.md](README.md) and [How the code is organised](CONTRIBUTING.md#how-the-code-is-organised) |
| Plan a change or write project names | [Contributing to garner](CONTRIBUTING.md#contributing-to-garner) and [Ground rules](CONTRIBUTING.md#ground-rules) |
| Run, configure or clean up local development | [Run it locally](CONTRIBUTING.md#run-it-locally) |
| Test a change or run the full check | [Check your changes](CONTRIBUTING.md#check-your-changes) |
| Write tests or fuzz parsing and decision code | [Writing tests](CONTRIBUTING.md#writing-tests) |
| Change the simulation, explore seeds or investigate a nightly failure | [The simulation](CONTRIBUTING.md#the-simulation) and the package comment in `internal/simulation/simulation_test.go` |
| Record the terminal interface or take screenshots | [tools/demo.sh](tools/demo.sh) and `newDemoWorld` in `internal/simulation/demo_test.go` |
| Change the terminal interface | [Terminal interface conventions](internal/tui/README.md#terminal-interface-conventions) |
| Change profiles or media-release decisions | [Profiles](docs/profiles.md#profiles), [profile schema](docs/profile.schema.json) and [How garner chooses releases](docs/releases.md#how-garner-chooses-releases) |
| Change imports or file ownership | [Files in the library](docs/library.md#files-in-the-library) and [The simulation](CONTRIBUTING.md#the-simulation) |
| Write or update documentation | [Documentation](CONTRIBUTING.md#documentation) |
| Change configuration or installation docs | [garner.example.toml](garner.example.toml), [Install garner](docs/install.md#install-garner) and [Compose guide](examples/compose/README.md#run-garner-with-docker-compose) |
| Change authentication or transport | [What garner protects](SECURITY.md#what-garner-protects) |
| Report a security problem | [Report a vulnerability](SECURITY.md#report-a-vulnerability) |
| Change branding | [garner's logo](docs/brand/README.md#garners-logo) |
| Open a pull request | [Pull request template](.github/pull_request_template.md) |
| Choose a release version or channel | [Versions](RELEASING.md#versions) and [Channels](RELEASING.md#channels) |
| Publish a version or handle a failed release | [From change to release](RELEASING.md#from-change-to-release) and [If a release fails](RELEASING.md#if-a-release-fails) |
| Change release tooling or prepare release notes | [How a release is built](RELEASING.md#how-a-release-is-built) |

## Before running garner

Before running either program, verify that the server configuration points only at the development stack. Check the terminal interface's connection before opening it, including any saved connection in `~/.config/garner/connection` or override in `GARNER_CONNECTION`. Keep secret values out of these checks' output.

## Code-change pointers

- For file changes in `internal/importer`, read the [importer guard rule](CONTRIBUTING.md#writing-tests).
- For SQL or migration changes, follow the [`mise run generate` guidance](CONTRIBUTING.md#generated-files).
- For dependency changes, follow the [`mise run notices` guidance](CONTRIBUTING.md#generated-files).
- For screen or key changes, follow [internal/tui/README.md](internal/tui/README.md).

## Private real-case tests

Before merging changes to `internal/release`, `internal/match` or `internal/selection`, run the maintainer's private cases as required by the [pull request template](.github/pull_request_template.md):

1. Set `GARNER_MATCH_CASES` to `tests/match-cases.json` in the maintainer's private `garner-research` repository.
2. Set `GARNER_SELECT_CASES` to `tests/select-cases.json` in that repository.
3. Set `GARNER_RELEASE_CASES` to `tests/release-cases.json` in that repository.
4. Run `go test ./...`.

If the private cases are unavailable, report that they were not run. Keep the cases and any real names in their output private.

## Review, commit and push

Use `jj` for version-control work in this colocated repository.

1. Inspect the intended diff for unrelated changes, secrets and real release names.
2. Before every commit, when CodeRabbit is available, run `coderabbit review --agent --base-commit <parent> --include-untracked`. Replace `<parent>` with the parent commit of the change being reviewed.
3. Address relevant review findings before committing.
4. Commit the reviewed change.
5. Before every push, run `mise run check` on the final tree.
6. Obtain the user's confirmation before pushing.

Commit subjects in the history are imperative and describe what changes for the user or the project, such as `Recognise more of SABnzbd's failures as the release's own`. Most commits have a body explaining why. The history uses British spellings such as "Recognise" and "catalogue".

When handing work back, report what was verified and what was not. Name any checks that could not be run, including CodeRabbit or the private cases.
