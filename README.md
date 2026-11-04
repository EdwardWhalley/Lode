# Lode

**Query data files and databases with SQL, right in your terminal.**

Point Lode at a CSV, a Parquet file or a database, type SQL, and browse the results. It's one download with nothing else to install, and it's read-only unless you say otherwise.

```
lode sales.csv
lode events.parquet
lode app.sqlite
```

## What it opens

| What | How you open it |
| --- | --- |
| CSV, TSV, JSON, NDJSON, Parquet | `lode sales.csv`, then query it as the table `sales` |
| SQLite | `lode app.sqlite` (also `.sqlite3` and `.db`) |
| PostgreSQL, MySQL, MariaDB | Save a connection, then `lode prod` ([see below](#connect-to-a-database)) |

## Install

Download the file for your system from the [Releases page](https://github.com/EdwardWhalley/lode/releases):

| System | File |
| --- | --- |
| Windows 10/11 | `lode_<version>_windows_amd64.exe` |
| macOS, Apple silicon | `lode_<version>_darwin_arm64` |
| macOS, Intel (untested) | `lode_<version>_darwin_amd64` |
| Linux, x86-64 (Ubuntu 22.04+, Debian 12+) | `lode_<version>_linux_amd64` |

Rename it to `lode` (`lode.exe` on Windows) and put it somewhere on your `PATH`. On macOS and Linux, also make it runnable with `chmod +x lode`.

The downloads aren't code-signed, so your system may warn you the first time:

- **Windows:** choose "More info", then "Run anyway".
- **macOS:** run `xattr -d com.apple.quarantine lode`.

## Get started

1. Open a file: `lode sales.csv`
2. Press `ctrl+r` to run the query in the editor, or type your own first.
3. Press `tab` to move to the results, then use the arrow keys to move around.
4. Press `enter` to see a whole value, `c` to copy it, or `ctrl+s` to export the results.

Press `F1` at any time to see every key, and `ctrl+q` to quit.

### The keys you'll use most

| Key | What it does |
| --- | --- |
| `ctrl+r` | Run the selected text, or else the statement under the cursor |
| `esc` | Cancel a running query |
| `tab` | Move between the editor, results and schema |
| `enter` | In the results, show the whole value |
| `c` / `r` | In the results, copy the cell / the row |
| `ctrl+s` | Export the results to `.csv` or `.json` |
| `ctrl+o` | Switch to another file or database |
| `ctrl+g` | Search your query history |
| `F1` | Show every key |
| `ctrl+q` | Quit |

## Connect to a database

Save your connections in `~/.config/lode/sources.toml` (on Windows, the same path inside your user folder):

```toml
[sources.prod]
dsn = "postgres://reader:${PROD_PASSWORD}@db.example.com/app"

[sources.shop]
dsn = "mysql://reader:${SHOP_PASSWORD}@db.example.com:3306/shop"

[sources.sales]
path = "~/data/sales.csv"
```

Then open one by name with `lode prod`.

You can also add a connection from inside Lode: press `ctrl+o`, then `n`. It tests the connection before saving it.

Keep passwords out of the file by writing `${NAME}`, which Lode fills in from that environment variable.

## Safe by default

- **Read-only.** The database itself refuses writes, not just Lode. To allow writes, use `--write` or add `write = true` to a saved connection, and the title bar will show `[write enabled]`. Files can never be written to.
- **For a database you must not change,** also connect with an account that can only `SELECT`. Read-only mode guards against accidents, not against a hostile database.
- **Limited rows.** A query shows at most 10,000 rows, so a big table can't flood your memory. Change this with `--max-rows`.
- **Exports never overwrite** an existing file.
- **Your queries are saved in plain text** in your history, at `~/.local/state/lode/history`, so a password typed into a query ends up there too. Add `history = false` to turn history off.

## Good to know

- `ctrl+r` runs one statement, so you can keep several queries in the editor without re-running the others. To run a whole script, select it first with `alt+a`.
- A transaction can't stay open between runs, because the next run may use another connection. Lode refuses one and tells you what to do instead.
- You can't use `COPY` on a file, because Lode never changes files. Use `ctrl+s` to export instead.
- A DuckDB `JSON` column isn't shown exactly as stored: its keys get sorted, and very large numbers lose digits. Write `col::VARCHAR` to see the original.
- Some terminals keep certain key combinations for themselves. If a key does nothing, `F1` lists the others.

## Settings

Add these to the top of `sources.toml`, before the first `[section]`:

```toml
theme = "classic"     # bubbletea, classic, light or mono
max_rows = 2000       # rows per query, up to 1,000,000
history = false       # don't save queries
mouse = false         # leave the mouse to your terminal
home = false          # skip the home page
```

Press `F2` to try the themes: `bubbletea`, `classic`, `light` and `mono`. `mono` has no colour, and Lode uses it automatically when `NO_COLOR` is set.

Run `lode --help` to see the command-line options.

## Mouse

The mouse is on by default: click to place the cursor or pick a cell, drag to select, and drag the borders to resize the panes.

To select text with your terminal's own selection instead, hold shift (`Shift+drag`, which works in Windows Terminal and most others). To turn the mouse off, use `--no-mouse` or `mouse = false`.

## All keys

<details>
<summary>Show every key</summary>

| Key | Action |
| --- | --- |
| `F1` | Show every key (press again, or `esc`, to close) |
| `ctrl+r` / `f5` | Run the Selection, else the Statement under the cursor |
| `esc` / `ctrl+c` | Cancel a query; `ctrl+c` copies a Selection or grid cell, else quits |
| `tab` | Cycle focus: editor → results grid → schema tree |
| arrows, `pgup`/`pgdn`, `home`/`end` | Grid: move the Highlight (h j k l too); tree: move |
| `c` / `r` | Grid: copy the highlighted cell / its row |
| `enter` | Grid: show the whole value of the highlighted cell |
| `enter` / `space` | Expand or collapse a table in the schema tree |
| `ctrl+o` | Switch Source: ↑/↓ and home/end move, enter switches, esc cancels |
| `n` | Selector: create a Source (type, form, test, save) |
| `e` | Selector: edit the highlighted Source (if in sources.toml) |
| `d` | Selector: delete the highlighted Source, after asking |
| `ctrl+s in the Source form` | After a failed test: save the Source anyway |
| `ctrl+s` | Export the rows shown to .csv or .json; never overwrites |
| `ctrl+↑` / `ctrl+↓` | Editor: step through this Source's history (alt+↑/↓ too) |
| `ctrl+g` | Search history of all Sources; enter loads a query, never runs it |
| `alt+/` | Editor: open suggestions now (they also open as you type) |
| `F2` | Next colour theme (keep one with theme = in sources.toml or --theme) |
| `shift+arrows`, `shift+home`/`end` | Editor: extend the Selection by character, row, or to the line ends |
| `ctrl+shift+←`/`→` | Editor: extend the Selection by word |
| `ctrl+shift+home`/`end` | Editor: extend the Selection to the start or end of the text |
| `ctrl+←`/`→` | Editor: move by word (alt+←/→ too) |
| `ctrl+alt+↑`/`↓` | Editor: add a cursor above / below (esc removes the extras) |
| `alt+a` | Editor: select all |
| `ctrl+z` / `ctrl+y` | Editor: undo / redo; a run of typing is one step |
| `ctrl+x` | Editor: cut the Selection (nothing happens with none) |
| `ctrl+v` | Editor: paste, replacing the Selection |
| `click` | Editor: place the cursor; grid: move the Highlight; tree: focus it |
| `drag` | Editor: select; past an edge it scrolls and keeps selecting |
| `double-click` / `triple-click` | Editor: select the word / the line |
| `shift+click` | Editor: extend the Selection from where it began |
| `drag a border` | Resize the panes by dragging the border between them |
| `wheel` | Scroll the pane under the pointer; the cursor stays put |
| `any key` | Home page: continue to the Source selector (ctrl+c quits) |
| `ctrl+q` | Quit |

</details>

## Build from source

Most people should download a release. To build it yourself you need Go and a C and C++ compiler (MinGW-w64 on Windows):

```
CGO_ENABLED=1 go build ./cmd/lode
```

On Windows, the compiler and the Go module cache must both be at paths with no spaces. If your user folder has a space in it, put the compiler in `C:\mingw64` and set `GOMODCACHE` to something like `C:\gomodcache`.

## License

[MIT](LICENSE)
