package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/oden"
)

// driver drives an otea.Shell synchronously without a TTY.
type driver struct{ sh *otea.Shell }

func newDriver(sh *otea.Shell, initCmd tea.Cmd, w, h int) *driver {
	sh.Engine.PrefixTimeout = 0
	sh.MaxFPS = 0
	d := &driver{sh: sh}
	d.send(tea.WindowSizeMsg{Width: w, Height: h})
	d.pump(initCmd)
	return d
}

func (d *driver) send(msg tea.Msg) {
	_, cmd := d.sh.Update(msg)
	d.pump(cmd)
}

func (d *driver) pump(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var m tea.Msg
	select {
	case m = <-done:
	case <-time.After(5 * time.Second):
		return
	}
	switch m := m.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			d.pump(c)
		}
	case tea.QuitMsg, nil:
	default:
		d.send(m)
	}
}

func (d *driver) keys(names ...string) {
	for _, n := range names {
		d.send(keyMsg(n))
	}
}

func (d *driver) typeText(s string) {
	for _, r := range s {
		if r == ' ' {
			d.send(keyMsg("space"))
		} else {
			d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
}

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	"delete": tea.KeyDelete,
}

func keyMsg(n string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		if rest, ok := strings.CutPrefix(n, "ctrl+"); ok && len(rest) > 0 {
			mod, n = mod|tea.ModCtrl, rest
		} else if rest, ok := strings.CutPrefix(n, "alt+"); ok && len(rest) > 0 {
			mod, n = mod|tea.ModAlt, rest
		} else if rest, ok := strings.CutPrefix(n, "shift+"); ok && len(rest) > 0 {
			mod, n = mod|tea.ModShift, rest
		} else {
			break
		}
	}
	if r, ok := namedKeys[n]; ok {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	if n == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " ", Mod: mod}
	}
	r := []rune(n)[0]
	if mod != 0 {
		return tea.KeyPressMsg{Code: r, Mod: mod}
	}
	return tea.KeyPressMsg{Code: r, Text: n}
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;:?]*[A-Za-z]|\x1b\][^\a\x1b]*(\a|\x1b\\)`)

func plain(s string) string { return ansiRe.ReplaceAllString(s, "") }

func maxLineW(s string) int {
	w := 0
	for _, l := range strings.Split(s, "\n") {
		w = max(w, textW(l))
	}
	return w
}

// smokeTool builds an isolated temp fixture workspace (manifest, hub, git repos).
// It never touches the real ~/proj or ~/hub.
func smokeTool() (*Tool, func(), error) {
	home, err := os.MkdirTemp("", "o-proj-smoke-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(home) }

	env := []string{
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Smoke",
		"GIT_AUTHOR_EMAIL=smoke@example.invalid",
		"GIT_COMMITTER_NAME=Smoke",
		"GIT_COMMITTER_EMAIL=smoke@example.invalid",
		"PATH=" + os.Getenv("PATH"),
	}
	tl := &Tool{
		Stdout: discard{},
		Stderr: discard{},
		Env:    env,
		Home:   home,
		Prog:   "o-proj",
		User:   "smoke",
		Now:    time.Now,
	}
	tl.Run = tl.exec

	mkRepo := func(name string, dirty bool) error {
		dir := filepath.Join(home, "proj", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# "+name+"\n\nFixture repository for o-proj smoke check.\n"), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte(licenseText("mit", 2026, "Smoke")), 0o644); err != nil {
			return err
		}
		for _, args := range [][]string{
			{"init", "-q", "-b", "main"},
			{"add", "."},
			{"commit", "-q", "-m", "initial"},
		} {
			cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false"}, args...)...)
			cmd.Env = env
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git %v: %w: %s", args, err, strings.TrimSpace(string(out)))
			}
		}
		if dirty {
			if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("uncommitted\n"), 0o644); err != nil {
				return err
			}
		}
		return nil
	}

	if err := mkRepo("alpha", false); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := mkRepo("beta", true); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := mkRepo("gamma", false); err != nil {
		cleanup()
		return nil, nil, err
	}

	hub := filepath.Join(home, "hub", "o")
	if err := os.MkdirAll(hub, 0o755); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := os.Symlink(filepath.Join(home, "proj", "alpha"), filepath.Join(hub, "alpha")); err != nil {
		cleanup()
		return nil, nil, err
	}

	entries := []Entry{
		{Name: "alpha", Path: "~/proj/alpha", Branch: "main", Origin: OriginNew, URL: "https://github.com/ofamtools/alpha.git"},
		{Name: "beta", Path: "~/proj/beta", Branch: "main", Origin: OriginAdopted},
		{Name: "gamma", Path: "~/proj/gamma", Branch: "main", Origin: OriginNew, Archived: true},
	}
	if err := tl.SaveManifest(entries); err != nil {
		cleanup()
		return nil, nil, err
	}
	return tl, cleanup, nil
}

func runSmoke(out io.Writer, dump bool) error {
	tl, cleanup, err := smokeTool()
	if err != nil {
		return err
	}
	defer cleanup()

	u := newUI(tl, UIConfig{})
	sh, initCmd := newShell(u)
	d := newDriver(sh, initCmd, 120, 32)
	page := func() Page { return pageOf(sh.State) }
	focused := func() string {
		p := page()
		id, _ := p.focusedName()
		return id
	}
	frame := func() string { return plain(sh.View().Content) }

	var results []string
	fails := 0
	check := func(name string, ok bool) {
		mark := "ok  "
		if !ok {
			mark, fails = "FAIL", fails+1
		}
		results = append(results, mark+" "+name)
	}

	f := frame()
	check("frame is 120x32", len(strings.Split(f, "\n")) == 32 && maxLineW(f) <= 120)
	check("initial async load populated projects", page().Loaded && len(page().Items) == 3 && len(page().Grid.Rows) == 2)
	check("markdown detail pane renders focused project", page().Detail && strings.Contains(f, "alpha") && strings.Contains(f, "Manifest"))

	d.keys("j")
	check("j moves to second project (beta)", focused() == "beta")

	d.keys("/")
	d.typeText("betx")
	check("/ filters while typing", page().Typing && len(page().Grid.Rows) == 0)
	d.keys("backspace")
	check("backspace refines filter to beta", page().Query == "bet" && len(page().Grid.Rows) == 1)
	d.keys("ctrl+a", "ctrl+k")
	check("readline ctrl+a ctrl+k clears filter", page().Query == "" && len(page().Grid.Rows) == 2)
	d.typeText("alpha")
	d.keys("enter")
	check("enter keeps filter in normal mode", !page().Typing && page().Query == "alpha" && len(page().Grid.Rows) == 1)
	d.keys("esc")
	check("esc clears filter", page().Query == "" && len(page().Grid.Rows) == 2)

	d.keys("o")
	check("o cycles sort to status", page().Sort == "status")
	d.keys("O")
	check("O reverses sort", page().Desc)
	d.keys("g", "b")
	check("g b groups by origin", page().Grid.GroupField == "origin")
	d.keys("g", "b", "g", "b", "g", "b", "g", "b")
	check("g b cycles back to ungrouped", page().Grid.GroupField == "")

	d.keys("a")
	check("a shows archived project (gamma)", page().ShowArchived && len(page().Grid.Rows) == 3)
	d.keys("a")
	check("a hides archived project again", !page().ShowArchived && len(page().Grid.Rows) == 2)

	d.keys("e")
	check("e opens edit form modal", len(sh.State.Modals) == 1 && sh.State.Modals[0].ID == modalForm)
	d.keys("esc")
	check("esc closes edit form", len(sh.State.Modals) == 0)

	d.keys("n")
	check("n opens new project form modal", len(sh.State.Modals) == 1 && sh.State.Modals[0].ID == modalForm)
	d.keys("esc")

	d.keys(":")
	check(": opens command palette", len(sh.State.Modals) == 1 && sh.State.Modals[0].ID == modalPalette)
	d.keys("esc", "?")
	check("? opens help overlay", len(sh.State.Modals) == 1 && strings.Contains(frame(), "project.new"))
	d.keys("esc")

	d.keys("g", "t")
	check("g t cycles theme", page().ThemeIdx == 1)
	d.keys("g", "d")
	check("g d cycles density", page().Density == ui.DensityCompact)
	d.keys("g", "t", "g", "t", "g", "d", "g", "d")

	if dump {
		f = frame()
		for _, line := range strings.Split(f, "\n") {
			fmt.Fprintln(out, strings.TrimRight(line, " "))
		}
		fmt.Fprintln(out)
	}
	for _, r := range results {
		fmt.Fprintln(out, r)
	}
	fmt.Fprintf(out, "smoke: %d/%d checks passed · %d projects · %d renders\n", len(results)-fails, len(results), len(page().Items), sh.Renders)
	if fails > 0 {
		return &oden.ExitError{Code: 1, Err: fmt.Errorf("smoke: %d check(s) failed", fails)}
	}
	return nil
}
