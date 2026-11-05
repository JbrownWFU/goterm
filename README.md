# goterm

A Ranger-style terminal file manager built with Bubble Tea, for the
cd / mkdir / write-markdown / hand-it-to-an-LLM loop. It has three columns
(parent folder, current folder, preview), renders markdown the way Glow does,
includes a built-in editor, and opens Claude Code or a shell in a new Windows
Terminal tab in whatever folder you're in.

Images (png, jpg, gif, bmp, webp, tiff) preview in color using half-block
characters, two pixels per cell. They are decoded in the background, so
scrolling past large photos stays smooth.

The colors are a Tokyo Night–style palette. Each file type has its own glyph
and color, and the bars show the current mode as a colored label.

## Install

Needs Windows 10 or 11. Windows Terminal is used for `c` / `s` (new tab with
Claude Code or a shell); everything else works in any terminal. Delete,
undo-from-Recycle-Bin and Explorer are Windows-only.

**Download:** grab `goterm.exe` from the Releases page, put it somewhere on
your `PATH` (e.g. `%USERPROFILE%\bin`), and run `goterm`. Windows SmartScreen
may warn because the exe isn't signed; "More info → Run anyway".

**Or build it** (Go 1.25+):

```powershell
git clone <repo url>
cd goterm
go install .
```

That puts `goterm.exe` in `%USERPROFILE%\go\bin`.

**Optional:** to have `q q` leave your shell in the folder you browsed to, add
the `gt` function from [gt.ps1](gt.ps1) to your PowerShell profile
(`notepad $PROFILE`), then use `gt` instead of `goterm`.

Settings live in `%APPDATA%\goterm\config.json` (created on first run).

## Keys

Press `?` in the app for the full list.

| Key | Action |
|---|---|
| `h j k l` / arrows | navigate (enter opens folders, or edits files) |
| `J` / `K` | scroll the preview |
| `enter` / `e` on an image | full-screen image view: `j`/`k` flip through the folder's images, `E` open with, `esc` back |
| `n` / `N` | new file (`.md` if no extension; uses a template; opens in the editor) / new folder (`a/b/c` ok) |
| `e` | built-in editor (`ctrl+s` save, `ctrl+p` markdown preview, `esc` close) |
| `E` | open with Notepad++, VS Code, Notepad, or the default app |
| `b` | bookmarks: `enter`/`1-9` jump, `t` open in new tab, `a` add current folder, `r` rename, `d` remove, `J`/`K` reorder |
| `B` | bookmark the current folder |
| `c` / `s` | Claude / shell in a new Windows Terminal tab here |
| `r` / `d` | rename (asks `y` to confirm) / send to Recycle Bin (asks `y` to confirm) |
| `u` / `ctrl+z` | undo, newest first, with a status line saying what was undone (see below) |
| `y` / `Y` | copy path of the selection / current folder |
| `f` / `ctrl+f` | find: fuzzy search this folder and everything below it (`enter` go to, `ctrl+o` open, `ctrl+r` re-index) |
| `S` | sort by name, modified, size or type; reverse; folders first or mixed (saved as your default) |
| `/` | filter the current folder (`esc` clears) |
| `.` / `R` | toggle dotfiles / refresh |
| `t` `x` `tab` `1-9` | new tab, close tab, next tab, jump to tab |
| `o` | open Explorer here with the selected file or folder highlighted |
| `q` `q` / `Q` `Q` | quit and cd there (via `gt`) / quit without cd. Each takes two presses in a row; `ctrl+c` twice also quits, and `ctrl+c` never closes the editor |

## Undo

`u` (or `ctrl+z`) steps back through everything goterm has changed this session, up to 50 steps:

| Action | What undo does |
|---|---|
| send to Recycle Bin | restores it from the Recycle Bin to where it was |
| rename | renames it back |
| new file | deletes it if untouched; sends it to the Recycle Bin if it was edited |
| new folder | deletes it if still empty of files; otherwise sends it to the Recycle Bin |
| editor save | puts the previous contents back |
| bookmark add / rename / remove / reorder | restores the previous list |
| sort change | restores the previous sort |
| close tab | reopens it |

Undo refuses rather than overwrite anything: if something now sits where a file would be restored, or a saved file has changed since, it stops and says why. Undo history isn't kept after you quit.

## Config

On first run, goterm writes `%APPDATA%\goterm\config.json`. In it you can change:
- the "open with" list (`openers`)
- the new-file template (`{{title}}`, `{{date}}`)
- the Windows Terminal commands (`claudeCommand`, `shellCommand`)
- the markdown style (`tokyo-night`, `dark`, `light`, `dracula`, ...)
- whether dotfiles are shown
- `sort` (normally set with `S`) and `findSkip`, the folder names find never looks inside (`node_modules`, `.git`, `.venv`, ...)
- `bookmarks` (normally managed with `b` / `B`, but you can edit them here too)

## Layout

| File | Contents |
|---|---|
| `main.go` | flags, program start, `--choosedir` output |
| `model.go` | state and key handling for each mode |
| `view.go` | rendering of columns, preview, bars, picker and help |
| `theme.go` | color palette, styles, file-type glyphs |
| `bookmarks.go` | saved locations: panel, keys, saving |
| `image.go` | image decoding, thumbnail cache, half-block rendering, full-screen viewer |
| `find.go` | recursive background index, fuzzy matching and the find screen |
| `sort.go` | sort orders, natural name sort, sort menu |
| `tab.go` | directory listing, filtering and cursor |
| `editor.go` | built-in editor (bubbles textarea) |
| `actions.go` | external launches (wt, openers, Explorer) and Recycle Bin |
| `undo.go` | the undo stack and how each action is reversed |
| `recyclebin.go` | finding and restoring items in the Windows Recycle Bin |
| `config.go` | config file and defaults |

Run the tests with `go test ./...`. One test sends throwaway temp files to the
real Recycle Bin and restores them.

## License

MIT, see [LICENSE](LICENSE).
