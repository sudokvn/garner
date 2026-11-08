# Install garner

This guide runs the server, `garnerd`, as a container next to Prowlarr, SABnzbd and optionally Jellyfin, and connects the terminal interface to it. The examples use Podman; the differences for Docker are noted. To run garner, Prowlarr and SABnzbd together with Docker Compose, use the [Compose example](../examples/compose) instead.

The examples assume these folders on the server. Adjust them to your setup:

| Folder on the server | Holds |
| --- | --- |
| `/etc/garner` | `garner.toml` and `garner.env` |
| `/srv/garner` | garner's database, backups and certificate; local disk, not a network share |
| `/srv/media/usenet` | SABnzbd's downloads |
| `/srv/media/library/tv` and `/srv/media/library/anime` | The library |

## Prepare the other services

1. If you use Jellyfin, install the TheTVDB plugin from the plugin catalogue and restart Jellyfin. In the settings of your TV and anime libraries, put TheTVDB first in the metadata order for shows, seasons and episodes. garner identifies shows and episodes by their TVDB IDs, and Jellyfin has to report them.
2. If you use Jellyfin, create an API key for garner under **Dashboard › API Keys**.
3. In SABnzbd, create the category `tv` under **Config › Categories**. garner sends its downloads with this category.
4. If garner will reach SABnzbd by a host name, such as `sabnzbd.home.lan`, add that name to `host_whitelist` under **Config › Special** in SABnzbd. SABnzbd refuses requests addressed to any other name, and garner then shows `Hostname verification failed`.
5. Keep SABnzbd's completed folder and the library on the same filesystem. garner moves each file with a rename, which fails across filesystems.
6. Create the library folders, here `/srv/media/library/tv` and `/srv/media/library/anime`. garner creates the show and season folders below them, but never the library folders themselves, so an unmounted share makes imports fail instead of filling the local disk.
7. Give garner write access to the library folders and SABnzbd's completed folder. garner runs as UID 1000 in the container. With rootless Podman and the `--userns=keep-id:uid=1000,gid=1000` option used below, that is your own user on the server. With Docker, it is UID 1000 on the server.
8. Note the API keys of Prowlarr (**Settings › General**) and SABnzbd (**Config › General**).

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
4. Set the addresses of Prowlarr, SABnzbd and Jellyfin. Without Jellyfin, delete the `[jellyfin]` section.
5. If SABnzbd or Jellyfin see the media under other paths than garner does, map them with `paths`, the service's path first: `paths = { "/data/usenet" = "/srv/media/usenet" }`. For Jellyfin, map every folder its libraries use, as [Taking over files](library.md#taking-over-files) explains.
6. Write the secrets to `/etc/garner/garner.env`. Without Jellyfin, leave out `JELLYFIN_API_KEY`:

   ```bash
   PROWLARR_API_KEY=...
   SABNZBD_API_KEY=...
   JELLYFIN_API_KEY=...
   ```

   The image brings garner's own TheTVDB key. To use a key of your own, add `TVDB_API_KEY`, and `TVDB_PIN` if your key needs a subscriber PIN.

7. Make the user who runs the container the owner of both files, and let only that user read `garner.env`:

   ```bash
   sudo chown "$USER": /etc/garner/garner.toml /etc/garner/garner.env
   chmod 600 /etc/garner/garner.env
   ```

   On a server with SELinux, rootless Podman can label only files its user owns; with `garner.toml` left to root, the container does not start.

`garner.toml` accepts no secrets. Instead of a variable such as `SABNZBD_API_KEY`, you can set `SABNZBD_API_KEY_FILE` to a file that holds the value.

## Start the server

1. Create the data folder and make your user its owner:

   ```bash
   sudo mkdir -p /srv/garner
   sudo chown "$USER": /srv/garner
   ```

2. Start the container with the newest version from the [releases](https://github.com/sudokvn/garner/releases). Pick one not marked Pre-release unless you run the [dev channel](#versions-and-channels):

   ```bash
   podman run -d --name garner \
     --userns=keep-id:uid=1000,gid=1000 \
     --read-only --tmpfs /tmp \
     --cap-drop all --security-opt no-new-privileges \
     -p 7979:7979 \
     -e TZ=Europe/Berlin \
     --env-file /etc/garner/garner.env \
     -v /etc/garner/garner.toml:/config/garner.toml:ro \
     -v /srv/garner:/data \
     -v /srv/media:/srv/media \
     ghcr.io/sudokvn/garner:<version>
   ```

   With Docker, leave out `--userns=…` and make UID 1000 the owner of `/srv/garner` instead: `sudo chown 1000:1000 /srv/garner`.

   On a server with SELinux enforcing, such as Fedora or RHEL, label the mounts so the container may use them. garner's own files get `:Z`: `-v /etc/garner/garner.toml:/config/garner.toml:ro,Z` and `-v /srv/garner:/data:Z`. The media, which other containers share, gets `:z`. An NFS mount cannot be relabelled; for it, add `--security-opt label=disable` instead. Never relabel a whole system folder such as `/etc`.
3. Check that garner runs:

   ```bash
   podman exec garner garnerd status
   ```

   The first line reads `● garner <version> · running since …`.

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
5. garner starts with the profile Original language, which wants each show in its original audio. For another language, set up a profile as [Manage profiles in garner](profiles.md#manage-profiles-in-garner) describes.

`garnerd token list` lists the tokens, and `garnerd token revoke <device>` disconnects a device.

In About, press `o` for the licence and source links for the server and terminal interface. The installer keeps the licence and notices beside the executable in `garner-licenses/` (normally `~/.local/bin/garner-licenses/`). Homebrew keeps them in the installed cask. Each release also provides a source archive with build instructions; you do not need it to run garner.

## Back up the database

`garnerd backup` writes a consistent copy of the database to `backups/` in the data folder while garner keeps running:

```bash
podman exec garner garnerd backup
```

Run it daily, for example from a systemd timer, and delete old copies. garner also backs up the database on its own before a new version changes the database's structure.

## Versions and channels

| Channel | Gets | Server image | Terminal interface |
| --- | --- | --- | --- |
| stable | stable releases, such as `v0.9.0` | `ghcr.io/sudokvn/garner:latest`, or the version | `install.sh`, or `brew install sudokvn/tap/garner` |
| dev | release candidates, such as `v0.9.0-rc.1`, and every stable release | `ghcr.io/sudokvn/garner:dev`, or the version | `install.sh --channel dev`, or `brew install sudokvn/tap/garner@dev` |

A release candidate is what the next release will be. It runs on the maintainer's own server before it becomes a stable release: at least a week for new features, a day for a release that only fixes. Run the dev channel to get new versions early and to report what breaks; run stable otherwise. Use the same channel for the server and the terminal interface.

To install the terminal interface from the dev channel:

```bash
curl -fsSL https://raw.githubusercontent.com/sudokvn/garner/main/install.sh | bash -s -- --channel dev
```

With Homebrew, the two channels are separate casks that cannot be installed together. To switch to dev, run `brew uninstall --cask garner`, then `brew install --cask sudokvn/tap/garner@dev`. To switch back, do the reverse. The terminal interface keeps its connection either way.

The first time a garner installed with Homebrew starts, macOS asks once in a window whether to open it, because it was downloaded from the internet. The terminal waits until you answer. `install.sh` installs it without this question.

Going back from dev to stable means running an older version until the next stable release. If a candidate changed the database, its release notes say so, and the older version refuses to start on it. Going back then needs the backup garner made before that change: `pre-migration-v<version>-<time>.db` in `backups/` in the data folder.

## Upgrade

1. Read the release notes of the new version.
2. Check that garner is idle, so the restart interrupts no import:

   ```bash
   podman exec garner garnerd status --json
   ```

   `work.idle` is `true` when nothing is in progress.
3. Remove the container and start it again with the new version's image, using the same `podman run` command.
