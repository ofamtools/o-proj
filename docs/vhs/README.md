# README demo

`demo.tape` records `assets/demo.gif`, the animation under the README header. It runs `o-proj` over a throwaway home of invented repositories that `seed.sh` writes.

Regenerate from the repository root (needs [VHS](https://github.com/charmbracelet/vhs) and Go; `brew install vhs`):

```sh
vhs docs/vhs/demo.tape
```

It takes under a minute and writes a GIF of about 25 seconds and under 400 KB.

## Your data stays out of it

Off camera, the tape makes a fresh `/tmp/oproj.XXXX` and runs `seed.sh` on it. The script builds `o-proj` into the sandbox's `~/bin` and writes an `env` file that points `HOME`, every `XDG_*` directory, `O_PROJ_MANIFEST`, `O_PROJ_HUB` and `PATH` inside it, points `O_PROJ_MTM` at a binary that does not exist, unsets `TUIOS_*`, `MTM_SERVER` and `O_PROJ_MTM_ARGS`, and turns off your git config. It makes three repositories under `~/proj` (`tea-shop`, `rice-cooker` with an uncommitted change, `miso-notes`), each with an invented `example` GitHub remote that is never contacted, and links the first two from `$HOME/hub/o`. The demo passes `--no-mtm` to `new` and never uses `--github`; no o-inventory is on PATH, so `new` skips the hook. The tape deletes the directory when it ends.

To look at the demo data by hand:

```sh
D=$(mktemp -d /tmp/oproj.XXXX) && docs/vhs/seed.sh "$D" && (. "$D/env" && o-proj seed && o-proj list --table); rm -rf "$D"
```

## Changing it

Edit the steps between `Show` and `Hide`. The screen holds about 28 lines: `clear` before a long output. To stay small, keep the window size, the run short and the pauses few. Check a few frames before committing, for example:

```sh
ffmpeg -i assets/demo.gif -vf fps=1 /tmp/frame%02d.png
```
