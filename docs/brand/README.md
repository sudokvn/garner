# garner's logo

The mark is a G that holds one block: an episode, gathered. The wordmark is set in Geist Mono Medium.

## The name

The name is garner, all lowercase, as in the wordmark, also at the start of a sentence and in headings. The programs keep their names in code font: `garner` is the terminal interface and `garnerd` the server.

## Files

| File | Use it for |
| --- | --- |
| `garner-logo-on-dark.svg` | The logo on dark backgrounds |
| `garner-logo-on-light.svg` | The logo on light backgrounds |
| `garner-logo-stacked-on-dark.svg`, `garner-logo-stacked-on-light.svg` | Square spaces, such as a sticker or a slide |
| `garner-mark.svg` | The mark alone on dark backgrounds |
| `garner-mark-ink.svg` | The mark alone on light backgrounds |
| `garner-mark-white.svg` | The mark alone on mint |
| `garner-icon.svg`, `garner-icon.png` | Avatars and app icons: the mark on an ink square; the PNG is 1024 px |
| `garner-social.png` | GitHub's social preview, 1280 × 640 px, set under the repository's Settings → General → Social preview |

The SVG files are the masters. They hold paths only, so they need no font. Render PNGs from them at the size you need.

## Colours

| Name | Hex | Use |
| --- | --- | --- |
| Mint | `#75B39C` | The mark on dark backgrounds; also the terminal interface's accent, `cMint` in `internal/tui/style.go` |
| Ink | `#0E0D15` | Dark backgrounds, and the whole logo on light backgrounds |
| Snow | `#EDF1F2` | The wordmark on dark backgrounds |

On light backgrounds, the logo is ink only: mint on white is too faint.

## Space and size

- Keep free space around the logo on every side, at least as wide as the block.
- Show the mark at 16 px or larger and the logo at 24 px high or larger. Below that, use the mark alone.

## Keep the logo as it is

Don't stretch, rotate or outline the logo, and don't add shadows or gradients. Don't move the block, and don't set "garner" in another font. To change the logo, change the SVG masters.

## Construction

The mark sits on a 256 grid. The ring has an outer radius of 106 and an inner radius of 62, so it is 44 wide, as wide as the block. A vertical line crosses the inner circle 44 above and 44 below the ring's centre; that line holds the ring's upper cut, the block's left edge and the inner edge of the ring's lower end. The block sits on the ring's horizontal centre line, with a gap of 22 above and below it. The whole mark is moved 4.8 to the right so that its outline is centred on the grid.

Geist Mono is licensed under the SIL Open Font License 1.1. The wordmark is converted to outlines.
