#!/usr/bin/env bash
# seed.sh DIR: build o-proj into DIR/home/bin and make a throwaway world in
# DIR for the README demo: HOME, the XDG directories, a hub with an o/
# directory of links, and three invented repositories under ~/proj, two of
# them already linked from the hub and one with an uncommitted change.
# Nothing reads or writes your own hub, manifest, mtm or GitHub. It writes
# DIR/env, which points HOME, the XDG directories, O_PROJ_MANIFEST,
# O_PROJ_HUB and PATH inside DIR.
#
#	D=$(mktemp -d /tmp/oproj.XXXX) && docs/vhs/seed.sh "$D" && . "$D/env" && o-proj seed
set -euo pipefail

D=${1:?usage: seed.sh DIR}
D=$(cd "$D" && pwd -P)
mkdir -p "$D"/{home/bin,home/proj,home/hub/o,config,data,cache,state}
GOWORK=off go build -o "$D/home/bin/o-proj" .

# O_PROJ_MTM names a binary that is not there: new would ask mtm for a work
# project, and the demo passes --no-mtm anyway. No o-inventory is on PATH,
# so new skips the hook.
cat > "$D/env" <<ENV
unset TUIOS_ENV TUIOS_SESSION TUIOS_PANE_ID TUIOS_SOCKET MTM_SERVER O_PROJ_MTM_ARGS
export HOME=$D/home XDG_CONFIG_HOME=$D/config XDG_DATA_HOME=$D/data
export XDG_CACHE_HOME=$D/cache XDG_STATE_HOME=$D/state
export O_PROJ_MANIFEST=$D/home/hub/repos.json O_PROJ_HUB=$D/home/hub/o O_PROJ_MTM=$D/none
export PATH=$D/home/bin:/usr/bin:/bin GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com
PS1='\$ '
cd "$D/home"
ENV
# shellcheck disable=SC1091
. "$D/env"

for r in tea-shop rice-cooker miso-notes; do
	git -C "$HOME/proj" init -q -b main "$r"
	printf '# %s\n' "$r" > "$HOME/proj/$r/README.md"
	git -C "$HOME/proj/$r" add README.md
	git -C "$HOME/proj/$r" commit -q -m "first commit"
	git -C "$HOME/proj/$r" remote add origin "git@github.com:example/$r.git"
done
printf 'Rinse the rice twice.\n' >> "$HOME/proj/rice-cooker/README.md"
ln -s "$HOME/proj/tea-shop" "$HOME/hub/o/tea-shop"
ln -s "$HOME/proj/rice-cooker" "$HOME/hub/o/rice-cooker"
