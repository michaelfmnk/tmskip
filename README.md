# tmskip

Keep rebuildable developer folders — `node_modules`, `build/`, `target/`,
virtualenvs, package-manager and Xcode caches — out of your Time Machine
backups, and keep that in sync as projects come and go.

tmskip walks the folders you tell it to, matches them against the rules in
**one config file**, and gives each match a *sticky* Time Machine exclusion
(the same one `tmutil addexclusion` sets). It remembers which exclusions are
its own, so when a rule no longer matches, the folder goes back into your
backups. No root, no `sudo`.

```
$ tmskip sync
+ /Users/me/Developer/shop/node_modules  (node, 412.3 MB)
+ /Users/me/Developer/api/build  (jvm-build, 96.0 MB)
- /Users/me/Developer/old/vendor  (no rule matches)
+2 −1 =40
→ see what's managed: tmskip list
```

## Install

```sh
brew install michaelfmnk/tap/tmskip
```

The formula installs the signed and notarized release binary, so a Full Disk
Access grant survives `brew upgrade`.

From source (Go 1.27+): `go install github.com/michaelfmnk/tmskip/cmd/tmskip@latest`.
Unsigned builds lose Full Disk Access on every rebuild.

## Get started

```sh
tmskip init
```

`init` finds your code folders (`~/Developer`, `~/Projects`, `~/IdeaProjects`, …),
writes `~/.config/tmskip/config.toml`, shows what it would exclude, and offers to
apply it, ask for Full Disk Access if needed, and turn on the hourly schedule.
Every step can be skipped.

By hand: `tmskip config reset` writes the default config, `tmskip sync -n`
previews, `tmskip sync` applies, `tmskip schedule on` runs it hourly.

## The schedule

`tmskip schedule on` installs a LaunchAgent that runs `tmskip sync` every
`[schedule] interval`. `tmskip status` shows the last run, and a scheduled run
posts one notification per new problem (`notify = false` turns that off). If you
move the binary or the config, or change the interval, any tmskip command tells
you to run `tmskip schedule on` again.

## Full Disk Access

Only needed when a root or fixed path is inside a folder macOS protects:
`~/Desktop`, `~/Documents`, `~/Downloads`, iCloud Drive, `~/Library/CloudStorage`,
or an external volume. The default paths don't need it. What matters is the
scheduled run's access; `tmskip doctor` shows it.

1. Open **System Settings → Privacy & Security → Full Disk Access**.
2. Click **+**, press **⌘⇧G**, paste the binary path (`tmskip doctor` prints it;
   for Homebrew it's `/opt/homebrew/bin/tmskip`) and add it. Make sure its switch is on.
3. After the next scheduled run, check `tmskip doctor`.

## Config

`~/.config/tmskip/config.toml` (or `--config`, `$TMSKIP_CONFIG`,
`$XDG_CONFIG_HOME/tmskip/config.toml`). **Nothing is hidden:** every rule tmskip
applies is in this file. Parsing is strict: an unknown key is an error, not a
silently dropped rule.

```toml
version = 1
roots   = ["~/Developer"]          # folders scanned by match rules
skip    = [".git"]                 # folder names never descended into or matched
keep    = ["~/Developer/notes"]    # never excluded; everything inside stays in backups
notify  = true                     # notification when a scheduled run hits a new problem

[schedule]
interval = "1h"                    # at least 5m

[[rule]]
name  = "node"
names = ["node_modules", ".next"]  # the folder's own name

[[rule]]
name     = "python-venv"
contains = ["pyvenv.cfg"]          # the folder contains one of these

[[rule]]
name       = "jvm-build"
names      = ["build", "target"]
parent_has = ["build.gradle", "build.gradle.kts", "settings.gradle",
              "settings.gradle.kts", "pom.xml"]   # a sibling file gates the match

[[rule]]
name  = "my-models"
paths = ["~/.lmstudio/models"]     # fixed paths, excluded wherever they are
# enabled = false
```

- A rule is either **fixed** (`paths`) or a **match** rule (`names`,
  `contains`, `parent_has`). Keys are AND-ed, values within a list OR-ed.
  Values are exact names, no globs. `parent_has` needs `names` or `contains`.
- `~` is expanded; relative paths are errors.
- A matched folder isn't descended into.
- tmskip never excludes `$HOME` or anything containing it, a root, anything
  containing a root or a `keep` path, or anything outside the roots and fixed paths.

Shipped groups: `node`, `python`, `python-venv`, `jvm-build`, `rust`, `swiftpm`,
`nobackup-marker` (any folder with a `.nobackup` file) and `dev-caches`.
`tmskip config defaults` prints them; `tmskip config add <group>` appends one.

## Commands

```
tmskip                        status (or a pointer to init when there's no config)
tmskip init                   guided setup
tmskip sync [-n]              reconcile exclusions with the config; -n = dry run with sizes
tmskip status                 last run, managed folders and size, problems, schedule
tmskip list [--sizes]  (ls)   folders tmskip manages, with their rule
tmskip why <path>             which rule / gate file / keep / guardrail decides a folder
tmskip doctor                 config, Time Machine destination, Full Disk Access, agent
tmskip schedule [on|off]      the LaunchAgent; bare = its state
tmskip uninstall              schedule off, re-include everything tmskip excluded, drop state
tmskip config defaults [grp]  shipped groups, marked with what you have
tmskip config add <grp>...    append shipped groups
tmskip config reset [--yes]   write the default config (.bak of the old one first)
tmskip config path            resolved config path and symlink target
```

Global flags: `--config <path>`, `--json` (status, list, why, doctor, sync),
`-v`, `-q`, `--version`. Exit codes: `0` ok, `1` error, `2` config missing or
invalid, `3` finished but some folders couldn't be read.

## How it works

- Exclusions are the sticky `com_apple_backup_excludeItem` attribute that
  `tmutil addexclusion` (without `-p`) writes, so they move with the folder.
- Every folder tmskip excludes also gets a `dev.tmskip.managed` attribute. That
  marker decides "is this mine?": folders you excluded yourself are never touched.
- A folder is re-included only when tmskip could read it and no rule matches.
  Unreadable folders and unplugged drives are reported and left alone; if a
  root, `keep` or fixed path can't be read, the run changes nothing.

## Uninstall

```sh
tmskip uninstall          # schedule off, re-include every folder tmskip excluded, remove its state
brew uninstall tmskip
```

`uninstall` leaves your config and logs. If some folders couldn't be read, it
says so and exits 3; run it again once they're reachable. A folder you moved
outside your roots keeps its exclusion. To undo one by hand:

```sh
xattr -p dev.tmskip.managed <folder>      # prints 1 if tmskip excluded it
tmutil removeexclusion <folder> && xattr -d dev.tmskip.managed <folder>
```

## Development

```sh
make check         # gofmt, vet, unit tests (in-memory filesystem, no real side effects)
make integration   # real xattrs + tmutil, only in temp dirs
make install       # versioned build into $(go env GOPATH)/bin
```

Releasing: see [RELEASING.md](RELEASING.md).

## License

MIT — see [LICENSE](LICENSE).
