# Terminal interface conventions

These are the conventions of `internal/tui`, garner's terminal interface. Follow them when you change a screen or add one, so every screen reads and behaves the same way. The interface uses [Bubble Tea](https://github.com/charmbracelet/bubbletea) v2 and [Lip Gloss](https://github.com/charmbracelet/lipgloss) v2.

## Size

Screens are designed for 120 by 36 cells (`designWidth` in `style.go`). A wider terminal keeps the layout at the left edge. Tables span the window, and their last column, usually State, takes the extra width. Framed panes and running text stay at 120 cells at most, so lines stay readable. On a narrower terminal, columns give way in a fixed order, and names keep at least `minName` cells.

## Screen layout

Every screen has the same skeleton:

1. **Row 1:** the breadcrumb, such as `garner › Library › Show`, with the last part bold. At the right: the counts of work in progress, such as `◐ 3 downloading · ● 1 confirming · ✗ 1 failed`, showing only counts above zero. While the server is unreachable, `✗ offline · reconnecting` in red takes their place.
2. **Row 2** is blank. Then either the category tabs (Library) or one line of labelled facts, a blank line, the column headers, a rule and the rows.
3. **Last two rows:** a rule in the border colour across the full width, then the key bar, a message after an action, or a prompt such as the `/` filter. The rule keeps a full screen apart from the keys; screens size their tables with `barHeight`.

Columns are three cells apart. Numbers are right-aligned. The first column is a single glyph. Column headers are subtle, except the sorted one, which shows `▾`, or `▴` when reversed. The cursor row has the select background across its full width. Rows that need nothing, such as complete shows or folded seasons, are subtle.

Success and info messages, such as `✓ Added Harbor Precinct`, disappear after 3 seconds or at the next key. Errors and work in progress, such as `Searching…`, stay until something replaces them.

## Kinds of surface

- **Full screen:** a place you navigate to and act from, such as Library, Show, Releases, Episode, Activity, Profiles, the profile editor, Import and About.
- **Framed pane:** a secondary block inside a screen, such as WHY under Releases. It has square corners in the border colour, an uppercase title in the subtle colour cut into the top edge (`┌─ WHY ───`), two cells of padding and the width of the table above it.
- **Overlay:** a question or a quick lookup, such as delete show, cancel download, profile or Help. It is a rounded box in the centre, with everything behind it in the faded colour and its keys inside the box. The cursor starts on the safe choice. Overlays have no `y` or `n` shortcuts, and `esc` closes them.
- **Bottom line:** keys, a message or a prompt. A screen never has a second status row.

## Glyphs

One set of glyphs marks states everywhere, in pipeline order (`glyphs` in `help.go`):

| Glyph | State |
| --- | --- |
| `·` | not aired |
| `○` | aired, nothing in the library |
| `◐` | downloading |
| `●` | processing, or importing into the media library |
| `✓` | in the library |
| `▲` | needs you: the file is in place, but Jellyfin did not confirm it |
| `✗` | failed |
| `~` | last resort |

## Colours

The palette is in `style.go`:

| Name | Colour | Used for |
| --- | --- | --- |
| fg | `#EDF1F2` | text |
| muted | `#9199A3` | secondary text |
| subtle | `#80848D` | headers, rows that need nothing |
| border | `#3D3D47` | rules and frames |
| select | `#242037` | cursor row |
| mint | `#75B39C` | keys in the key bar (bold), success, garner's mark |
| amber | `#F79926` | work in progress, needs you, files below the profile's first preference |
| red | `#F54D47` | failures, offline |
| iris | `#9BA0E3` | links, underlined |
| faded | `#3A3A44` | everything behind an overlay |

The interface sets no background colour, so it uses the terminal's own.

## Mark

garner's mark, the logo from `docs/brand` drawn with half blocks in mint (`markLines` in `mark.go`), is 17 cells wide and 9 rows tall. It appears in two places:

- **Empty library:** centred above "No shows yet", with two blank rows above and below it. It needs 14 rows in the table; with fewer, the message stands alone at the left as usual.
- **About:** left of the name and versions, which then start in column 24. It needs 80 columns.

The mark shows only in terminals with 256 colours or more. With 16 colours or `NO_COLOR`, both screens look as they would without it. Keep the mark to these two places.

## About

About keeps licence access to one line, `AGPLv3 · source and licence`, followed by the linked `Metadata: TheTVDB` credit. Press `o` to open the licence and source lookup. It offers the AGPLv3 text and the corresponding-source archive for each build: the connected server and the terminal interface. Missing source locations are stated, not guessed from the upstream repository.

## Keys

Every screen lists its keys in a `help()` method, and the Help overlay (`?`) shows them with the keys that every screen has. The key bar shows the most important ones. When you add a key, add it to the screen's `help()`.

The key bar stays in place as the cursor moves. A key whose label or action depends on the row, such as `enter` on the Show screen or `x cancel` in Activity, uses `keySlot`: its label keeps the room of the widest one, and a row without the action shows the key greyed out instead of leaving it out.

Some keys mean the same everywhere:

- `j k` move, `gg G` jump to the top or bottom, `ctrl+d u` move by half a page.
- `enter` opens, `esc` goes back, `A` opens Activity, `P` opens Profiles, `?` opens Help, `q` quits from the Library.
- `/` filters as you type in the bottom line; `enter` keeps the filter and `esc` clears it.
- `s` sorts by the next column and `I` reverses the order.
- `d` deletes and is never a navigation key. `j k` never leave a pane; `tab` moves between panes.
- While text is typed or a question is open, the screen takes every key, including `?` and `A`.

## Text

- Sentences from the server are shown as they are: summaries, reasons, details, recovery hints, failures and notes. Do not rephrase them in the interface. A summary may follow `missing · ` with its first letter lowercased, as in `missing · found 21 matching releases`.
- The interface's own strings are few: key labels, frame titles, overlay questions and short states such as `missing`, `airs in 5 days` or `importing into the media library…`.
- Release names are cut in the middle, keeping the group after the last hyphen (`…-GRP`). Reasons are cut at the end. Paths wrap after a `/` and are never cut.
- Where the server cannot tell something yet, show that honestly, never a made-up number.

## Test data

Use made-up shows and release names in fixtures and tests, such as `Harbor Precinct` and `Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP`. Real release names stay out of the repository.
