# Build garner from its source archive

AGPL releases publish their corresponding source as `garner-VERSION-source.tar.gz` next to the binaries on its [GitHub release](https://github.com/sudokvn/garner/releases). The archive holds the source of the programs garner ships: the terminal interface `garner`, the server `garnerd` with the Turso library it embeds, and FFprobe in the server's container image. That includes the code garner did not write: the Go modules, Turso's native library and its Rust crates, and the FFmpeg release FFprobe is built from. It does not hold the source of the compilers, of Rust's standard library or of the Alpine packages the image is based on and built with; [What the builds need](#what-the-builds-need) lists them. This guide builds each part from the archive.

`tools/source.sh` builds the archive from the committed `HEAD`, excluding local edits and untracked files. The release workflow builds the server image from the same archive.

## What the archive holds

Unpack the archive:

```bash
tar -xzf garner-VERSION-source.tar.gz
cd garner-VERSION-source
```

| Folder | Holds |
| --- | --- |
| `garner/` | garner's source at the release's commit, with every Go module it builds with in `vendor/` |
| `ffmpeg/` | The FFmpeg release the image's FFprobe is built from, as downloaded from ffmpeg.org |
| `turso/` | GitHub's archive of the Turso commit `garnerd`'s native library is built from, every Rust crate it builds with in `vendor/`, the build scripts of Turso's platform module in `turso-go-platform-libs/`, `turso.env` with the build settings, and `build.sh` |

Each build section below starts in the archive's root, `garner-VERSION-source/`. Return there before starting another section.

garner is licensed under the GNU Affero General Public License version 3 only: see `garner/LICENSE` and `garner/NOTICE`. Third-party code keeps its own licence. `garner/THIRD-PARTY-NOTICES.txt` lists the code compiled into the programs, Turso's native library included, with its licence texts. FFmpeg is under the LGPL 2.1 or later.

## What the builds need

The archive holds the source. The compilers and the system libraries that the builds link come from elsewhere:

| Part | Needs | Network |
| --- | --- | --- |
| `garner` and `garnerd` | Go, in the version `garner/mise.toml` names; no C compiler | None |
| The server image | Podman or Docker; the base images the Dockerfile pins by digest; Alpine's `build-base`, `xz`, `zlib-dev` and `zlib-static` for FFprobe, and `libgcc` at run time | For the base images, the Alpine packages, the FFmpeg release and musl's copyright notice |
| Turso's native library | The Rust image `turso/turso.env` pins, with Rust and Cargo 1.88.0 and GCC; Alpine's `musl-dev` | To add `musl-dev` to the image; none for the build |

Turso's library also links Rust's standard library, which comes with the Rust toolchain. Its source is in Rust's source release for the version `turso/turso.env` names, such as `rustc-1.88.0-src.tar.gz`, and in the toolchain's `rust-src` component.

FFprobe links zlib and musl statically, and Turso's library links musl and `libgcc_s` dynamically, from Alpine's packages at build time. garner does not pin those packages, so a later build may get newer ones.

## Build the terminal interface

You need the Go version that `garner/mise.toml` names. The modules come from `vendor/`, so the build needs no network.

```bash
cd garner
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=VERSION -X main.commit=COMMIT" \
  -o garner ./cmd/garner
```

Replace `VERSION` and `COMMIT` with the release's version and commit, or leave the `-ldflags` out. Set `GOOS` and `GOARCH` to build for another platform. A release also sets `github.com/sudokvn/garner/internal/buildinfo.sourceURL` to its source archive, using another `-X` flag. For a modified build you distribute or expose over a network, provide its matching source and set this URL to where users can get it. Do not point modified builds at an unmodified upstream archive. Leaving the URL empty makes About report that no source link was supplied; it does not remove your source-distribution obligations.

## Build the server

To build `garnerd` without the network, build it for Linux x86 with the tags the Dockerfile uses:

```bash
cd garner
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags musl,timetzdata -trimpath -o garnerd ./cmd/garnerd
```

To build the container image, run Podman or Docker in `garner/`:

```bash
podman build --build-arg VERSION=VERSION -t localhost/garner:VERSION .
```

The image build takes the Go modules from `vendor/`. The Dockerfile downloads FFmpeg and checks it against the checksum of the release in `ffmpeg/`, then configures FFprobe with the options in its `ffprobe` stage. Your FFprobe matches the release's in its FFmpeg source and options. It is not guaranteed to match byte for byte, because Alpine's compiler, zlib and musl may have changed since.

## Build Turso's native library

`garnerd` embeds Turso's library for Linux x86 with musl, `libturso_sync_sdk_kit.so`, from the Go module `github.com/tursodatabase/turso-go-platform-libs`. That module holds the compiled library only. `turso/` holds its source and builds it the way the module's `build_libs.sh` does, without the network.

1. In `turso/`, build the build environment: the Rust image `turso.env` names, with musl's development files. This step needs the network.

   ```bash
   cd turso
   . ./turso.env
   printf 'FROM %s\nRUN apk add --no-cache musl-dev\n' "$RUST_IMAGE" | podman build -t turso-build -
   ```

2. Build the library without the network. On hosts enforcing SELinux, add `:Z` to the volume mount (`-v "$PWD:/turso:Z"`) so the container can read and write the source folder.

   ```bash
   podman run --rm --network=none -v "$PWD:/turso" -w /turso turso-build sh build.sh
   ```

   On an ARM machine, add `--platform linux/amd64` to both commands; the build then runs emulated and takes much longer. You should see the checksums of the library you built and of the one `garnerd` embeds:

   ```text
   built:    …  /turso/out/libturso_sync_sdk_kit.so
   embedded: d36ed759…  libs/linux_amd64_musl/libturso_sync_sdk_kit.so
   ```

The two checksums are not expected to match. Upstream builds the library with a toolchain installed when it builds, in a folder of its own, and Rust records paths and build details in the library. The Turso commit and the Rust version come from upstream's platform module, which says it holds the libraries built from that Turso version; `turso/turso.env` records how garner checked that. `out/crates.txt` lists the crates compiled into your build.

To run `garnerd` with your own library, return to the archive's root and replace the embedded library and its checksum, which names the folder `garnerd` unpacks it into. On macOS without `sha256sum`, use `shasum -a 256` in its place below. The commands replace the library and checksum only after hashing succeeds:

```bash
libs=garner/vendor/github.com/tursodatabase/turso-go-platform-libs/libs/linux_amd64_musl
checksum=$(sha256sum turso/out/libturso_sync_sdk_kit.so) &&
  cp turso/out/libturso_sync_sdk_kit.so "$libs/" &&
  printf '%s\n' "${checksum%% *}" >"$libs/libturso_sync_sdk_kit.so.sha256"
```

Then build the server again.

## Replace FFprobe

FFprobe is a separate program in the image, at `/usr/local/bin/ffprobe`. To run garner with an FFprobe you built yourself, from `ffmpeg/` or a changed FFmpeg, build a static binary for Linux x86 with the options in the Dockerfile's `ffprobe` stage. Then put it into the image, either by changing the Dockerfile or with an image of your own:

```dockerfile
FROM ghcr.io/sudokvn/garner:VERSION
COPY ffprobe /usr/local/bin/ffprobe
```
