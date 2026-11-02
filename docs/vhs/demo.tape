# The README demo: o-proj over a throwaway home of invented repositories.
# Regenerate from the repository root:  vhs docs/vhs/demo.tape
#
# seed.sh builds o-proj and makes everything under a fresh /tmp/oproj.XXXX:
# HOME, XDG dirs, a hub with o/ links, a manifest and three repositories. The
# recording never reads or writes your own hub, manifest, mtm or GitHub.

Output assets/demo.gif

Require go

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 1080
Set Height 600
Set Padding 16
Set WindowBar Colorful
Set Framerate 15
Set TypingSpeed 45ms
Set CursorBlink false

# Off camera: build and seed.
Hide
Type "export D=$(mktemp -d /tmp/oproj.XXXX) && docs/vhs/seed.sh $D && . $D/env && clear"
Enter
Wait@90s /\$$/
Show
Sleep 600ms

# Three repositories on disk, two of them linked from the hub, none recorded.
Type "ls ~/proj ~/hub/o"
Enter
Sleep 1.5s

# seed finds the linked ones; adopt records the third and links it.
Type "o-proj seed"
Enter
Sleep 1.2s
Type "o-proj adopt ~/proj/miso-notes"
Enter
Sleep 1.2s
Type "o-proj list --table"
Enter
Sleep 2s
Type "o-proj status"
Enter
Sleep 2.5s
Type "clear"
Enter
Sleep 300ms

# new: mkdir, git init, verify, commit, record, link.
Type "o-proj new o-tea --no-mtm"
Enter
Sleep 2.5s
Type "o-proj project list --format table"
Enter
Sleep 2s
Type "o-proj sync --dry-run"
Enter
Sleep 2.5s

Hide
Type "cd / && rm -rf $D"
Enter
Sleep 500ms
