# o-proj: agent guide

`o-proj` (so `o proj`) keeps the family's repositories on disk and the hub: it creates a repository with a verified `git init`, records it in the manifest `$HOME/hub/repos.json` (`$O_PROJ_MANIFEST`), links it from `$HOME/hub/o` (`$O_PROJ_HUB`), clones and syncs the manifest on another machine, and reports git status. Repository management stays separate from mtm work contexts.

It is not mtm's `project` noun (a work context: tasks, notes, panes). The two meet only through each other's command line: `o proj new` runs `mtm catalog`, `mtm projects directories` and `mtm project new NAME --dir DIR`; mtm runs `o-proj list` for its known directories. o-proj never imports a matomeru package or opens mtm's database.

## Plans and todos

Keep planning work in the family planning workspace, separate from this repository. Do not add a TODO.md or docs/plans/ to this repo.

## Where does X live

| Concern | File |
|---|---|
| `Tool`, `main`, the oden app, `new`, `check`, the toplevel guard | `main.go` |
| The manifest: `Entry`, read/save, `record`, `describe` | `manifest.go` |
| `list`, `adopt` | `repos.go` |
| Hub links: `link`, `unlink`, `~` expansion, the AGENTS.md pointer | `hub.go` |
| `clone`, `sync` | `sync.go` |
| `status` (moved from o-family): the report and its git inspection | `report.go`, `status.go` |
| `seed`: the one-time import into the manifest | `seed.go` |
| The mtm project `new` asks for, through mtm's CLI | `mtm.go` |
| The ochazuke runtime: the `project` and `template` nouns, `catalog` | `shell.go` |
| Tables: each noun's catalog columns and its rows' cells (license and hub-link detection) | `table.go` |
| Templates, licenses | `templates.go`, `license.go` |

## Invariants

- Record first: once the repository exists, `new` never fails because of the hub link, the o-inventory hook or mtm; each is one output line.
- The manifest is written only by `new`, `adopt` and `seed`; an entry is never rewritten by them. Paths are stored with `~`.
- Links and sync never replace anything already at a hub name, never touch a dirty repository's files, and pull only with `--ff-only`. `--dry-run` writes nothing (no clone, link, fetch or pull).
- Tests run in a temp home with a temp manifest; no test reaches the real hub, mtm or GitHub (gh calls are recorded; mtm is a fake script).
- Every commit: `gofmt -l . && go vet ./... && go test -race ./...` green, `go run . catalog` answers, `o proj check`. No amends.

Build: `go build -o ~/bin/o-proj .`. ochazuke (`v0.28.0`) and oden (`v0.1.1`) are required at their tags; an untracked `go.work` (`go work init . && go work use ../oden ../ochazuke`) builds against the sibling checkouts, and `GOWORK=off` builds from the tags as a fresh clone does.
