# Terminal interface conventions

These are the conventions of `internal/tui`, garner's terminal interface. Follow them when you change a screen or add one, so every screen reads and behaves the same way. The interface uses [Bubble Tea](https://github.com/charmbracelet/bubbletea) v2 and [Lip Gloss](https://github.com/charmbracelet/lipgloss) v2.

## Size

Screens are designed for 120 by 36 cells (`designWidth` in `style.go`). A wider terminal keeps the layout at the left edge, except the Library: from 160 columns (`previewWidth` in `preview.go`) its table takes the left half and a preview of the show under the cursor the right, behind a rule in the border colour. Tables span the window, and their last column, usually State, takes the extra width. Framed panes and running text stay at 120 cells at most, so lines stay readable. On a narrower terminal, columns give way in a fixed order, and names keep at least `minName` cells.

## Screen layout

Every screen has the same skeleton:

1. **Row 1:** the breadcrumb, such as `garner › Library › Show`, with the last part bold. At the right: the counts of work in progress, such as `◐ 3 downloading · ● 1 confirming · ✗ 1 failed`, showing only counts above zero. While the server is unreachable, `✗ offline · reconnecting` in red takes their place.
2. **Row 2** is blank. Then either the category tabs (Library) or one line of labelled facts, a blank line, the column headers, a rule and the rows.
3. **Last two rows:** a rule in the border colour across the full width, then the key bar, a message after an action, or a prompt such as the `/` filter. The rule keeps a full screen apart from the keys; screens size their tables with `barHeight`.

Columns are three cells apart. Numbers are right-aligned. The first column is a single glyph. Column headers are subtle, except the sorted one, which shows `▾`, or `▴` when reversed. Rows that need nothing, such as complete shows or folded seasons, are subtle.

## Cursor

The row under the cursor has the select background across its full width and a bold mint `>` in the margin, in the cell before the glyph column. The chosen answer of a question and the chosen entry of a pick list carry the same `>` in their first cell. In a terminal without colour, such as with `NO_COLOR`, reverse video takes the place of the select background and the `>` stays.

Draw every cursor with `cursor` in `style.go`, never with the select colour itself. A table row starts with `margin()`, styles each part with `bg` and ends with `fill`. An option of a question or a pick list starts with `option`; `choice` draws a whole answer. A setting's chosen value, as in the profile editor, is bold, so it shows without colour too.

Success and info messages, such as `✓ Added Harbor Precinct`, disappear after 3 seconds or at the next key. Errors and work in progress, such as `Searching…`, stay until something replaces them.

## Kinds of surface

- **Full screen:** a place you navigate to and act from, such as Library, Show, Releases, Episode, Activity, Profiles, the profile editor, Import and About.
- **Framed pane:** a secondary block inside a screen, such as WHY under Releases. It has square corners in the border colour, an uppercase title in the subtle colour cut into the top edge (`┌─ WHY ───`), two cells of padding and the width of the table above it.
- **Overlay:** a question or a quick lookup, such as delete show, cancel download, profile, Help or the command palette. It is a rounded box in the centre, with everything behind it in the faded colour and its keys inside the box. The cursor starts on the safe choice. Overlays have no `y` or `n` shortcuts, and `esc` closes them.
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

In a table's State column, a download that runs, waits for Jellyfin or failed at a step shows the pipeline strip before its state text: six cells for the six steps of the Episode screen (`strip.go`). In colour they are squares: mint done, amber current, red failed, the border colour not reached. Without colour their shapes differ: `■` done, `▸` current, `✗` failed, `□` not reached. The strip replaces the progress bar; the percentage stays.

## Colours

The palette is in `style.go`. It has a dark set and a light set of the same roles:

| Name | Dark | Light | Used for |
| --- | --- | --- | --- |
| fg | `#EDF1F2` | `#1C2024` | text |
| muted | `#9199A3` | `#545B66` | secondary text |
| subtle | `#80848D` | `#6B7079` | headers, rows that need nothing |
| border | `#3D3D47` | `#C4C8CF` | rules and frames |
| select | `#242037` | derived | cursor row, chosen option |
| mint | `#75B39C` | `#2D7A5E` | keys in the key bar (bold), success, garner's mark |
| amber | `#F79926` | `#A65400` | work in progress, needs you, files below the profile's first preference |
| red | `#F54D47` | `#C42B26` | failures, offline |
| iris | `#9BA0E3` | `#4A50B8` | links, underlined |
| faded | `#3A3A44` | `#C9CCD1` | everything behind an overlay |

The interface sets no background colour, so it uses the terminal's own. On start it asks the terminal for that colour (`tea.RequestBackgroundColor`) and `usePalette` picks the set: the light one on a light background, with the select background blended from the terminal's background and a little violet. A terminal that does not answer keeps the dark set. Use the palette's variables and styles, never colour values of your own, and read them when drawing: they change once the terminal answers.

The profile editor's changes pane tints two kinds of row: what a setting was, with red blended into black on a dark terminal or white on a light one, and what it is now, with mint blended the same way (`changeTint` in `profile_changes.go`). Without colour, the words `was` and `now` tell the rows apart.

With `NO_COLOR` or a terminal without colour, Lip Gloss drops every colour but keeps bold and reverse video. The cursor then shows in reverse video; see [Cursor](#cursor).

## Mark

garner's mark, the logo from `docs/brand` drawn with half blocks in mint (`markLines` in `mark.go`), is 17 cells wide and 9 rows tall. It appears in two places:

- **Empty library:** centred above "No shows yet", with two blank rows above and below it. It needs 14 rows in the table; with fewer, the message stands alone at the left as usual.
- **About:** left of the name and versions, which then start in column 24. It needs 80 columns.

The mark shows only in terminals with 256 colours or more. With 16 colours or `NO_COLOR`, both screens look as they would without it. Keep the mark to these two places.

## About

About keeps licence access to one line, `AGPLv3 · source and licence`, followed by the linked `Metadata: TheTVDB` credit. Press `o` to open the licence and source lookup. It offers the AGPLv3 text and the corresponding-source archive for each build: the connected server and the terminal interface. Missing source locations are stated, not guessed from the upstream repository.

## Keys

Every screen lists its keys in a `help()` method, and the Help overlay (`?`) shows them with the keys that every screen has. The key bar shows the most important ones, keys in mint bold and labels in muted. When you add a key, add it to the screen's `help()`.

When the key bar is wider than the terminal, `fitKeys` leaves out whole keys by importance (`keyImportance` in `style.go`): `j k` first, then the screen's own keys from the right, then `enter`, `esc` and `q`, and `?` last of all. The keys that stay keep their order, so write a key bar in the order it reads best at full width.

The key bar stays in place as the cursor moves. A key whose label or action depends on the row, such as `enter` on the Show screen or `x cancel` in Activity, uses `keySlot`: its label keeps the room of the widest one, and a row without the action shows the key greyed out instead of leaving it out. A key that acts on a row, such as `enter`, `i` or `d`, is greyed out the same way when no row is under the cursor: a filter that matches nothing, an empty category, a show without episodes, a search that found nothing, an empty Activity or no profiles.

A letter means one action across the program, so the keys a user learns on one screen hold on the next. `TestOneJobPerLetter` checks that `s` and `r` keep their jobs below on every screen that lists them.

Some keys mean the same everywhere:

- `j k` or `up down` move, `gg G` or `home end` jump to the top or bottom, `ctrl+d u` or `pgdown pgup` move by half the rows the list shows. Every list with a cursor takes these keys through `listKeys` in `style.go`, and `TestListKeys` walks each list; add a new one there. While text is typed, as in the `/` filter or the command palette, only `up down`, `home end` and `pgdown pgup` move; in Add show, `home` and `end` move in the search field instead. In the profile editor, `ctrl+d u` and `pgdown pgup` scroll down to the unsaved changes instead.
- `enter` opens, such as an episode, a show or, on the Episode screen, the releases of its last search without searching again. `esc` goes back, `A` opens Activity, `P` opens Profiles, `?` opens Help, where `enter` opens About, `q` quits from the Library.
- `:` opens the command palette (`palette.go`). With nothing typed it lists the screen's actions from its `help()` that run on the current row, then what works anywhere. A screen answers for its keys in `action`, which its key bar uses to grey keys out, so a key the bar greys out is not offered, nor a key that acts on a row when there is none, and a key whose meaning depends on the row, such as `s` on the Show screen, carries the key bar's label. Typing filters them and adds shows, profiles and the open show's episodes. Choosing an action hands its key to the screen, so a new key in `help()` appears there by itself. Entries whose keys move the cursor, such as `j k`, `h l` or `tab`, are left out.
- `n` opens the next episode that needs the user (`needs.go`): failures not cleared first, then files Jellyfin did not confirm. Pressed again on that episode, it moves on and wraps around, and the bottom line counts the queue.
- `/` filters as you type in the bottom line, where a list offers a filter, as in the Library's show tabs; `enter` keeps the filter and `esc` clears it.
- `s` always searches for releases: the episode or season under the cursor on the Show screen, the episode under the cursor on the Library's Upcoming tab and in its preview, the episode on the Episode screen, and once more on Releases. It has no other job, so on the Library's show tabs it does nothing.
- `r` always retries or refreshes the thing in front of the user: the import on the Episode screen, the show's data from TheTVDB on Show details.
- `o` sorts by the next column and `I` reverses the order, where a list offers sorting, as in the Library's show tabs. The Upcoming tab, which lists episodes by day, has neither, nor `/`. On Show details, Add show's details and About, `o` opens a link instead.
- `i` opens the details of the show under the cursor, in the Library on every tab, and of the open show on the Show screen.
- `d` deletes and is never a navigation key.
- `tab` steps to the next section of a view and `shift+tab` to the one before: the category in the Library, the season on the Show screen, the answer of a question or pick list. It does the same at every width, also from inside a pane. In the Library, `1` to `4` also jump to a category.
- `l` and `h` move into and out of a pane beside a list, such as the Library's preview from 160 columns; `right` and `left` do the same, and `esc` also leaves. Below 160 columns, where the Library shows no pane, they do nothing there. On the Show screen, which has no pane, `h l` fold and unfold a season. `j k` never leave a pane.
- While text is typed or a question is open, the screen takes every key, including `?`, `A`, `:` and `n`.

## Text

- Sentences from the server are shown as they are: summaries, reasons, details, recovery hints, failures and notes. Do not rephrase them in the interface. A summary may follow `missing · ` with its first letter lowercased, as in `missing · found 21 matching releases`.
- The interface's own strings are few: key labels, frame titles, overlay questions and short states such as `missing`, `airs in 5 days` or `importing into the media library…`.
- Release names are cut in the middle, keeping the group after the last hyphen (`…-GRP`). Reasons are cut at the end. Paths wrap after a `/` and are never cut.
- Where the server cannot tell something yet, show that honestly, never a made-up number.

## Test data

Use made-up shows and release names in fixtures and tests, such as `Harbor Precinct` and `Harbor.Precinct.S01E01.German.DL.1080p.WEB-GRP`. Real release names stay out of the repository.
