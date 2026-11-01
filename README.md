<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/garner-logo-on-dark.svg">
    <img alt="garner" src="docs/brand/garner-logo-on-light.svg" height="56">
  </picture>
</h1>

garner downloads TV shows and anime from Usenet and checks every file before it reaches your library.

https://github.com/user-attachments/assets/a7a9075a-8e3d-48ea-ac9f-43af65058ade

- **Checks every file.** FFprobe confirms the audio language, resolution and runtime. If a file fails, garner downloads another release. With Jellyfin, garner also waits until Jellyfin identifies the episode.
- **Explains every decision.** Every release in a search shows its verdict and the reasons for it.
- **Watches your shows.** garner downloads new and missing episodes and, if the show's profile wants it, upgrades files when a better release turns up.
- **Handles anime numbering.** Scene and absolute episode numbers come from [TheXEM](https://thexem.info).
- **Keeps the files you have.** When you add a show, garner takes over its existing files instead of downloading them again.

## Compared with Sonarr

garner does less than [Sonarr](https://sonarr.tv) 4 (for now) but checks more:

- garner rejects a downloaded file with the wrong audio language and tries another release. Sonarr can reject a release for the language in its name, but not a downloaded file for its audio.
- garner shows why it accepts, rejects and ranks each release. Sonarr shows reasons for rejections only.
- A garner profile is a ranked list of languages and resolutions. Sonarr uses quality profiles and scored custom formats.
- garner has no web interface, and tools built for Sonarr, such as Jellyseerr and Bazarr, do not work with it.

## What you need

- A Linux server on x86 with Podman or Docker
- [Prowlarr](https://prowlarr.com) with at least one Newznab indexer
- [SABnzbd](https://sabnzbd.org) with a Usenet provider
- SABnzbd's downloads and the library on one filesystem, because garner moves files by renaming them. The [TRaSH Guides folder structure](https://trash-guides.info/File-and-Folder-Structure/) works.
- Optionally [Jellyfin](https://jellyfin.org) with the TheTVDB plugin, so garner can confirm each import and find existing files in Jellyfin's folders

garner brings its own TheTVDB key. To use your own, set `TVDB_API_KEY`.

## Get started

1. Set up the server, `garnerd`, with the [install guide](docs/install.md). To run garner, Prowlarr and SABnzbd together with Docker Compose, use the [Compose example](examples/compose) instead.
2. Install the terminal interface, `garner`, on your Mac or Linux machine:

   ```bash
   curl -fsSL https://raw.githubusercontent.com/trucke/garner/main/install.sh | bash
   ```

   Run it again to update. On a Mac, you can use `brew install trucke/tap/garner` instead. To get release candidates early, use the [dev channel](docs/install.md#versions-and-channels).

3. On the server, run `garnerd token create <device>`. Then run `garner` and paste the connection code.
4. Press `a` to add a show. Press `?` on any screen for its keys.

## Profiles

A show's profile sets the audio languages and resolutions garner accepts, best first. A new installation starts with Original language: the show's original audio, 1080p before 720p. garner also ships German; German, English fallback; English; and Original, English dub.

Press `P` to manage profiles, and `p` on a show's page to change its profile or its type, TV or anime. You can import profiles from a link and share your own; [Profiles](docs/profiles.md) describes the format.

Some rules apply to every profile. For example, garner never downloads a remux or a 2160p release on its own. [How garner chooses releases](docs/releases.md) lists them all.

## Existing files

When you add a show, garner takes over the video files that clearly belong to its missing episodes. It looks in its own folder for the show and, with Jellyfin, in every folder where Jellyfin keeps the show.

From then on, garner treats these files like its own downloads: an upgrade replaces them, and deleting the show with its files deletes them. It leaves every other file in the library alone. [Files in the library](docs/library.md) has the details.

## Responsible use

garner automates searching and downloading. It provides no content and no indexers. Download only what you have the right to download where you live.

## Contributing

Bug reports and pull requests are welcome; [CONTRIBUTING.md](CONTRIBUTING.md) explains the development setup. To report a security problem, follow [SECURITY.md](SECURITY.md).

## Data sources

Metadata provided by [TheTVDB](https://thetvdb.com). Please consider adding missing information or subscribing.

Scene numbering and alternative titles come from [TheXEM](https://thexem.info). Reviewed corrections to that data live in `internal/catalog/corrections.toml`.

## License

garner is released under the [MIT license](LICENSE). The Go modules the released binaries are built with are listed with their licenses in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt), which ships with them. The container image also contains an FFprobe built from [FFmpeg](https://ffmpeg.org) under the LGPL 2.1 or later; its license text is in the image at `/usr/share/licenses/ffprobe`.
