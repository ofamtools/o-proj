<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.svg">
    <img alt="o-proj" src="assets/logo-light.svg" width="247">
  </picture>
  <br>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ofamtools/o-proj"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/ofamtools/o-proj"></a>
</p>

<p align="center">The family's repositories on disk: create them with a verified git init, record them in one manifest, link them from the hub, and clone, sync and check them on any machine.</p>

<p align="center"><img alt="o-proj in a throwaway home: seeding the manifest from hub links, adopting a repository, the manifest and git status tables, a new project and a dry-run sync" src="assets/demo.gif" width="720"></p>

o-proj (`o proj` under [oden](https://github.com/ofamtools/oden)'s `o`, or `o-proj`) keeps the family's repositories: it creates a repository with a verified `git init`, records it in the manifest `$HOME/hub/repos.json`, links it from `$HOME/hub/o`, clones and syncs the manifest on another machine, and reports git status. It moved out of matomeru (`cmd/o-proj`, history kept by `git subtree split`) and folds in o-family's status report.

## Install

Module `ofam.dev/o-proj`; requires Go 1.27 or newer.

```sh
go install ofam.dev/o-proj@latest
```

or, from a checkout:

```sh
gofmt -l . && go vet ./... && go test -race ./...
go build -o ~/bin/o-proj .
```

ochazuke (`v0.28.0`) and oden (`v0.1.1`) are required at their tags. An untracked `go.work` (`go work init . && go work use ../oden ../ochazuke`) builds against the sibling checkouts; `GOWORK=off` builds from the tags as a fresh clone does.

## Usage

```sh
o proj new NAME [--root DIR] [--template T] [--existing] [--github] [--public] [--license mit|none] [--hub DIR|none] [--no-mtm]
o proj adopt PATH [--name NAME]         # record an existing repository and link it
o proj list [--table]                   # the manifest (JSON by default)
o proj link NAME | --all                # $HOME/hub/o/NAME -> DIR, from the manifest
o proj unlink NAME | --all
o proj clone [NAME...]                  # clone what is missing on this machine, then link
o proj sync [NAME...] [--pull] [--dry-run]   # clone, link, fetch, report dirty/behind; --pull fast-forwards clean ones
o proj status [DIR...] [-d|--dirty] [-c|--clean] [-j|--json]
o proj seed [--mtm-json FILE] [--inventory FILE] [--hub DIR] [--dry-run]
o proj check [DIR]                      # refuse a directory whose git toplevel is $HOME or above ~/proj
o proj templates
o proj catalog                          # the project and template nouns, for nabe
```

`new` also takes `--private` (the default for `--github`) and `--family=false` (alias `-o`) to skip the family hub link. Templates come from `$XDG_CONFIG_HOME/matomeru/templates` (else `~/.config/matomeru/templates`): a directory or a `.tar.gz`, whose `*.tmpl` files render with Go `text/template` and lose the suffix.

`--github` creates the repository under `$O_PROJ_GITHUB_OWNER` (default `ofamtools`), and a template's `{{.Module}}` is `$O_PROJ_MODULE_PREFIX` followed by NAME (default `ofam.dev/`, so `ofam.dev/NAME`).

The demo above, as text: a throwaway home with three repositories under `~/proj`, two of them linked from `$HOME/hub/o`, none recorded yet (`/private/tmp/oproj.uK3x` is the demo's temporary directory). Hub paths are shown with `$HOME` for readability:

```console
$ o-proj seed
seed: rice-cooker -> ~/proj/rice-cooker (hub)
seed: tea-shop -> ~/proj/tea-shop (hub)
seed: added 2 to $HOME/hub/repos.json
$ o-proj adopt ~/proj/miso-notes
manifest: miso-notes -> ~/proj/miso-notes in $HOME/hub/repos.json
link: $HOME/hub/o/miso-notes -> /private/tmp/oproj.uK3x/home/proj/miso-notes
$ o-proj list --table
NAME         PATH                BRANCH  ORIGIN   URL
miso-notes   ~/proj/miso-notes   main    adopted  git@github.com:example/miso-notes.git
rice-cooker  ~/proj/rice-cooker  main    adopted  git@github.com:example/rice-cooker.git
tea-shop     ~/proj/tea-shop     main    adopted  git@github.com:example/tea-shop.git
$ o-proj status
REPO         STATE  TRACKED  CHANGED  STAGED  UNSTAGED  NEW  BRANCH  DIRECTORY
miso-notes   clean  1        0        0       0         0    main    ~/proj/miso-notes
rice-cooker  dirty  1        1        0       1         0    main    ~/proj/rice-cooker
tea-shop     clean  1        0        0       0         0    main    ~/proj/tea-shop

3 directories: 1 dirty, 2 clean, 0 unavailable. Showing 3.
3 tracked files; 1 changed paths (0 staged, 1 unstaged, 0 new, 0 conflicts).
$ o-proj new o-tea --no-mtm
mkdir: created directory '/private/tmp/oproj.uK3x/home/proj/o-tea'
git init: /private/tmp/oproj.uK3x/home/proj/o-tea
verify: git toplevel is /private/tmp/oproj.uK3x/home/proj/o-tea
write: /private/tmp/oproj.uK3x/home/proj/o-tea/AGENTS.md
write: /private/tmp/oproj.uK3x/home/proj/o-tea/.gitignore
write: /private/tmp/oproj.uK3x/home/proj/o-tea/LICENSE
commit: e3536df chore: new project o-tea
manifest: o-tea -> ~/proj/o-tea in $HOME/hub/repos.json
link: $HOME/hub/o/o-tea -> /private/tmp/oproj.uK3x/home/proj/o-tea
hook: skipped (o-inventory not on PATH)
mtm: skipped (--no-mtm)
cd /private/tmp/oproj.uK3x/home/proj/o-tea
$ o-proj sync --dry-run
miso-notes: clean
o-tea: clean
rice-cooker: dirty (1 changed)
tea-shop: clean
dry run: 4 repositories: 0 cloned, 0 to clone, 0 missing, 1 dirty, 0 behind, 0 pulled, 0 failed
$ o-proj check ~
o-proj: STOP: /private/tmp/oproj.uK3x/home is not in a git repository; create it with o-proj new NAME (or --existing)
```

`o proj project list` and `o proj template list` answer rows (`{id, label, data, cells}`) that a host draws as tables: the catalog declares each noun's `columns` and every row carries one cell per column.

```console
$ o-proj project list --format table
name         origin   path                license  linked
miso-notes   adopted  ~/proj/miso-notes   none     yes
o-tea        o-proj   ~/proj/o-tea        mit      yes
rice-cooker  adopted  ~/proj/rice-cooker  none     yes
tea-shop     adopted  ~/proj/tea-shop     none     yes
```

| Noun | Columns (name never drops; when narrow, project drops origin, license, linked, then path; template drops kind, then path) |
| --- | --- |
| project | name, origin, path (with `~`), license (`mit`, `other`, `none`; empty when the directory is not on this machine), linked (`yes`, `no`, or `other` when something else is at `$HOME/hub/o/NAME`) |
| template | name, kind (`dir` or `tar.gz`), path |

## Concepts

### The manifest

`$HOME/hub/repos.json` (`$O_PROJ_MANIFEST`), a JSON array sorted by name:

```json
[{ "name": "oden", "url": "git@github.com:ofamtools/oden.git", "path": "~/proj/oden", "branch": "main", "origin": "adopted" }]
```

`path` keeps `~` and is expanded per machine; `origin` is `o-proj` (made by `new`) or `adopted`. `list` adds the expanded `dir`. The manifest is written only by `new`, `adopt` and `seed`, and they never rewrite an entry.

### Hub links

The hub links under `$HOME/hub/o` (`$O_PROJ_HUB`) are generated from the manifest (`link --all`, `sync`) and are not committed. Links and sync never replace anything already at a hub name, never touch a dirty repository's files, and pull only with `--ff-only`. `--dry-run` writes nothing (no clone, link, fetch or pull).

### Record first

Once the repository exists, `new` never fails because of the hub link, the o-inventory hook or mtm; each is one output line, as in the demo. `check` is the guard to run before the first commit in any directory: the home directory may itself be a git repository, and a directory without its own `git init` would commit into it silently.

### Seed

`seed` is the one-time import into the manifest from what recorded the repositories before it existed: the hub's links (`--hub`), o-inventory's `repos.json` (`--inventory`) and a saved `o-proj list` from the time o-proj wrote mtm's store (`--mtm-json`; only rows with origin `o-proj`). It adds missing names only and never rewrites an entry. A name's directory comes from the first source that has it: the manifest, the hub link, repos.json, the mtm export. Sources that disagree on a directory are printed as conflict lines and nothing is written (exit 1); adopt the right directory first, then seed again.

## In the family

- **mtm.** o-proj is not mtm's `project` noun (a work context: tasks, notes, panes). `o proj new` asks mtm for a work project, through its command line only: when `mtm catalog` offers `project.new` and `mtm projects directories` shows no project with this directory, it runs `mtm project new NAME --dir DIR`. `--no-mtm` skips it; any failure is a warning. `$O_PROJ_MTM` and `$O_PROJ_MTM_ARGS` (for example `--data /tmp/x.db`) point it at another mtm. mtm runs `o-proj list` for its known directories. o-proj never imports a matomeru package or opens mtm's database.
- **o-inventory.** `new` installs o-inventory's post-commit hook in the new repository when `o-inventory` is on PATH. o-inventory reads the manifest (and `o-proj list`) for the family's repositories and projects, and writes each repository's generated `CHANGELOG.md`; that file is not edited by hand.
- **ochazuke and oden.** `o proj` is an oden subcommand of `o`; the `project` and `template` nouns are an ochazuke runtime, and `o proj catalog` describes them for a host such as nabe that mounts this bin.

## Logo and demo

The logo is a tansu, a Japanese chest of drawers with every directory in its own drawer, in teal; its drawers and handles are drawn on an 8-unit grid so the icon stays a chest at 16 px. `python3 docs/logo/build.py` writes `assets/icon.svg`, `assets/logo-light.svg` and `assets/logo-dark.svg`, with the wordmark drawn in the family's lettering (ochazuke's and matomeru's; `--check` confirms the glyphs still redraw theirs). `vhs docs/vhs/demo.tape` records `assets/demo.gif` in a throwaway home; see [docs/vhs/README.md](docs/vhs/README.md).

MIT license.
