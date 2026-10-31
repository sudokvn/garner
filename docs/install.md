# Install garner

This guide sets up garner's server, `garnerd`, as a container next to Prowlarr, SABnzbd and Jellyfin, and connects the terminal interface to it. The examples use Podman; Docker works the same way, with the differences noted. To set up garner, Prowlarr and SABnzbd together with Docker Compose, use the [Compose example](../examples/compose) instead.

The examples assume these folders on the server. Adjust them to your setup:

| Folder on the server | Holds |
| --- | --- |
| `/etc/garner` | `garner.toml` and `garner.env` |
| `/srv/garner` | garner's database and backups; local disk, not a network share |
| `/srv/media/usenet` | SABnzbd's downloads |
| `/srv/media/library/tv` and `/srv/media/library/anime` | The library Jellyfin shows |

## Prepare the other services

1. In Jellyfin, install the TheTVDB plugin from the plugin catalogue and restart Jellyfin. In the settings of your TV and anime libraries, put TheTVDB first in the metadata order for shows, seasons and episodes. garner identifies shows and episodes by their TVDB IDs, and Jellyfin has to report them.
2. In Jellyfin, create an API key for garner under **Dashboard › API Keys**.
3. In SABnzbd, create the category `tv` under **Config › Categories**. garner sends its downloads with this category.
4. If garner will reach SABnzbd by a host name, such as `sabnzbd.home.lan`, add that name to `host_whitelist` under **Config › Special** in SABnzbd. SABnzbd refuses requests addressed to any other name; garner then shows `Hostname verification failed` for SABnzbd.
5. Make sure that SABnzbd's completed folder and the library are on the same filesystem. garner moves each file with a rename, which fails across filesystems.
6. Make sure that garner can write the library folders and SABnzbd's completed folder. garner runs as UID 1000 in the container. With rootless Podman and the `--userns=keep-id:uid=1000,gid=1000` option used below, that is your own user on the server. With Docker, it is UID 1000 on the server.
7. Note the API keys of Prowlarr (**Settings › General**) and SABnzbd (**Config › General**).

## Configure garner

1. Copy `garner.example.toml` from the repository to `/etc/garner/garner.toml`.
2. Set the folders as garner sees them inside the container:

   ```toml
   data_dir = "/data"

   [library]
   tv = "/srv/media/library/tv"
   anime = "/srv/media/library/anime"
   ```

3. Set the address garner listens on and the address clients use. With these settings, garner serves HTTPS with its own certificate, and the connection code lets clients verify it:

   ```toml
   [server]
   listen = "0.0.0.0:7979"
   url = "https://garner.home.lan:7979"
   ```

   To use your own certificate or a reverse proxy instead, follow the comments in the `[server]` section.
4. Set the addresses of Prowlarr, SABnzbd and Jellyfin. When SABnzbd or Jellyfin see the media under other paths than garner does, map them with `paths`, for example `paths = { "/data/usenet" = "/srv/media/usenet" }`.
5. Write the secrets to `/etc/garner/garner.env`:

   ```bash
   PROWLARR_API_KEY=...
   SABNZBD_API_KEY=...
   JELLYFIN_API_KEY=...
   ```

   garner's image brings its own TheTVDB key. To use a key of your own, add `TVDB_API_KEY`, and `TVDB_PIN` if your key needs a subscriber PIN.

6. Make the user who runs the container the owner of both files, and let only that user read `garner.env`:

   ```bash
   sudo chown "$USER": /etc/garner/garner.toml /etc/garner/garner.env
   chmod 600 /etc/garner/garner.env
   ```

   On a server with SELinux, rootless Podman can label only files its user owns; with `garner.toml` left to root, the container does not start.

Secrets never go into `garner.toml`. Instead of the environment, `garnerd` also reads each secret from the file that `NAME_FILE` names, such as `SABNZBD_API_KEY_FILE`.

## Start the server

1. Create the data folder and make your user its owner:

   ```bash
   sudo mkdir -p /srv/garner
   sudo chown "$USER": /srv/garner
   ```

2. Start the container. Pick the newest version from the [releases](https://github.com/trucke/garner/releases), one not marked Pre-release unless you run the dev channel (see [Versions and channels](#versions-and-channels)):

   ```bash
   podman run -d --name garner \
     --userns=keep-id:uid=1000,gid=1000 \
     --read-only --tmpfs /tmp \
     -p 7979:7979 \
     -e TZ=Europe/Berlin \
     --env-file /etc/garner/garner.env \
     -v /etc/garner/garner.toml:/config/garner.toml:ro \
     -v /srv/garner:/data \
     -v /srv/media:/srv/media \
     ghcr.io/trucke/garner:<version>
   ```

   With Docker, leave out `--userns=…` and make UID 1000 the owner of `/srv/garner` instead: `sudo chown 1000:1000 /srv/garner`.

   On a server with SELinux enforcing, such as Fedora or RHEL, label the mounts so the container may use them. garner's own files get `:Z`: `-v /etc/garner/garner.toml:/config/garner.toml:ro,Z` and `-v /srv/garner:/data:Z`. The media, which other containers share, gets `:z`. An NFS mount cannot be relabelled; for it, add `--security-opt label=disable` instead. Never relabel a whole system folder such as `/etc`.
3. Check that garner runs:

   ```bash
   podman exec garner garnerd status
   ```

   The first line reads `● garner <version> · running since …`, followed by the address clients use, the TLS mode, the database and the work in progress. Whether Prowlarr, SABnzbd, Jellyfin and TheTVDB answer shows on the terminal interface's About screen, after you connect it.

To start garner with the server, run the container from a systemd unit, such as a Podman Quadlet `.container` file with the same settings.

## Connect the terminal interface

1. Create a token for the device you use garner from:

   ```bash
   podman exec garner garnerd token create laptop
   ```

   The command prints a connection code and a token. Both are shown only once.
2. Install `garner` on that device as the [README](../README.md#get-started) describes, then run `garner`.
3. Paste the connection code. The library opens.
4. Press `?`, then `a`, to open the About screen. It shows whether Prowlarr, SABnzbd, Jellyfin and TheTVDB answer, and why not when one of them does not.
5. Press `P` to see the profiles. garner starts with Original language, which wants each show in its original audio. To download in another language, press `a` to start a profile from a shipped one, such as German or English, then `u` to make it the profile for new shows. [Choose which releases garner accepts](../README.md#choose-which-releases-garner-accepts) says more.

`garnerd token list` lists the tokens, and `garnerd token revoke <device>` disconnects a device.

## Back up the database

`garnerd backup` writes a consistent copy of the database to `backups/` in the data folder while garner keeps running:

```bash
podman exec garner garnerd backup
```

Run it daily, for example from a systemd timer, and delete old copies. garner also backs up the database on its own before a new version changes the database's structure.

## Versions and channels

garner comes in two channels:

| Channel | Gets | Server image | Terminal interface |
| --- | --- | --- | --- |
| stable | stable releases, such as `v0.9.0` | `ghcr.io/trucke/garner:latest`, or the version | `install.sh`, or `brew install trucke/tap/garner` |
| dev | release candidates, such as `v0.9.0-rc.1`, and every stable release | `ghcr.io/trucke/garner:dev`, or the version | `install.sh --channel dev`, or `brew install trucke/tap/garner@dev` |

A release candidate is what the next release will be. It runs on the maintainer's own server before it becomes a stable release: a week for new features, a day for a release that only fixes. Run the dev channel to get new versions early and to report what breaks; run stable otherwise. Use the same channel for the server and the terminal interface.

To run the terminal interface from the dev channel:

```bash
curl -fsSL https://raw.githubusercontent.com/trucke/garner/main/install.sh | bash -s -- --channel dev
```

With Homebrew, the two channels are separate casks that cannot be installed together: run `brew uninstall --cask garner`, then `brew install --cask trucke/tap/garner@dev`, and the other way round to switch back. The terminal interface keeps its connection either way.

Going back from dev to stable means going back to an older version until the next stable release. When a candidate changed the database, that needs the backup garner made before it changed it; its release notes say so.

## Upgrade

1. Read the release notes of the new version.
2. Check that garner is idle, so the restart interrupts no import:

   ```bash
   podman exec garner garnerd status --json
   ```

   `work.idle` is `true` when nothing is in progress.
3. Remove the container and start it again with the new version's image, using the same `podman run` command.
