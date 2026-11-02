package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The hub is one directory of links to the manifest's repositories
// ($HOME/hub/o/NAME -> DIR), generated from the manifest on each machine: link
// --all recreates them, so they are not committed. Links are absolute
// (DIR is the entry's path expanded for this machine).

// NoHub is the --hub value that skips the link.
const NoHub = "none"

// DefaultHub is $O_PROJ_HUB, else $HOME/hub/o.
func (t *Tool) DefaultHub() string {
	if v := lookup(t.Env, "O_PROJ_HUB"); v != "" {
		return v
	}
	return filepath.Join(t.Home, "hub", "o")
}

// PlansDir is the family's plans, todos and roadmap: o-plans beside the
// hub directory, written with ~ for home.
func (t *Tool) PlansDir() string {
	return t.tilde(filepath.Join(filepath.Dir(t.expand(t.DefaultHub())), "o-plans"))
}

// agentsMD is the AGENTS.md a new project starts with: its name and a
// pointer to the family's plans and todos.
func (t *Tool) agentsMD(name string) string {
	plans := t.PlansDir()
	return "# " + name + "\n\nCreated by o-proj.\n\n" +
		"## Plans and todos\n\n" +
		"Family plans, todos and the roadmap live in `" + plans + "/` (`README.md` is the index). " +
		"Read it before planning work; add or update items there. " +
		"Do not add a TODO.md or docs/plans/ to this repo.\n"
}

// expand resolves ~ and a leading ~/ against the tool's home.
func (t *Tool) expand(p string) string {
	if p == "~" && t.Home != "" {
		return t.Home
	}
	if rest, ok := strings.CutPrefix(p, "~/"); ok && t.Home != "" {
		return filepath.Join(t.Home, rest)
	}
	return p
}

// tilde abbreviates the home directory to ~ (the home as given, or with
// its symlinks resolved: /var is /private/var on macOS).
func (t *Tool) tilde(p string) string {
	if t.Home == "" {
		return p
	}
	for _, home := range []string{t.Home, real(t.Home)} {
		if !within(p, home) {
			continue
		}
		if rel, err := filepath.Rel(home, p); err == nil {
			if rel == "." {
				return "~"
			}
			return "~/" + rel
		}
	}
	return p
}

// Link outcomes; a link that already points at the directory counts as linked.
const (
	linked = "linked"
	kept   = "kept"
)

// link makes hub/name -> dir, creating hub when missing. Anything already
// at hub/name that is not a link to dir is kept as it is.
func (t *Tool) link(hub, name, dir string) (string, error) {
	hub, err := filepath.Abs(t.expand(hub))
	if err != nil {
		return "", err
	}
	path := filepath.Join(hub, name)
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			t.say("keep: %s exists (not a link)", t.tilde(path))
			return kept, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if target == dir || samePath(target, dir) {
			t.say("link: %s -> %s (exists)", t.tilde(path), dir)
			return linked, nil
		}
		t.say("keep: %s exists -> %s", t.tilde(path), target)
		return kept, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(hub, 0o755); err != nil {
		return "", err
	}
	if err := os.Symlink(dir, path); err != nil {
		return "", err
	}
	t.say("link: %s -> %s", t.tilde(path), dir)
	return linked, nil
}

// LinkCounts is link's summary.
type LinkCounts struct {
	Linked  int `json:"linked"`
	Kept    int `json:"kept"`
	Missing int `json:"missing"`
}

// LinkProjects links one manifest entry (name) or every one (all) into
// hub, one line per entry, then the summary. An entry whose directory is
// not on disk is reported as missing and not linked (sync clones it).
func (t *Tool) LinkProjects(hub, name string, all bool) (LinkCounts, error) {
	var n LinkCounts
	if all == (name != "") {
		return n, fmt.Errorf("usage: %s link NAME | --all [--hub DIR]", t.Prog)
	}
	if hub == NoHub {
		return n, errors.New("link needs a hub directory, not --hub none")
	}
	entries, err := t.Manifest()
	if err != nil {
		return n, err
	}
	if !all {
		if entries, err = t.pick(entries, []string{name}); err != nil {
			return n, err
		}
	}
	for _, e := range entries {
		outcome, err := t.linkEntry(hub, e)
		if err != nil {
			return n, err
		}
		switch outcome {
		case kept:
			n.Kept++
		case linked:
			n.Linked++
		default:
			n.Missing++
		}
	}
	t.say("linked %d, kept %d, missing %d", n.Linked, n.Kept, n.Missing)
	return n, nil
}

// linkEntry links e when its directory exists; else "missing".
func (t *Tool) linkEntry(hub string, e Entry) (string, error) {
	dir := t.Dir(e)
	if _, err := os.Stat(dir); err != nil {
		t.say("missing: %s %s does not exist", e.Name, e.Path)
		return "missing", nil
	}
	return t.link(hub, e.Name, dir)
}

// Link is the inline command: link NAME | --all [--hub DIR].
func (t *Tool) Link(args []string) error {
	name, hub, all, err := t.hubArgs("link", args)
	if err != nil {
		return err
	}
	_, err = t.LinkProjects(hub, name, all)
	return err
}

// hubArgs parses link's and unlink's arguments: [NAME] [--all] [--hub DIR].
func (t *Tool) hubArgs(verb string, args []string) (name, hub string, all bool, err error) {
	flags := flag.NewFlagSet(t.Prog+" "+verb, flag.ContinueOnError)
	flags.SetOutput(t.Stderr)
	allFlag := flags.Bool("all", false, verb+" every repository in the manifest")
	hubFlag := flags.String("hub", t.DefaultHub(), "the hub directory the links are in ($O_PROJ_HUB, else ~/hub/o)")
	words, err := parse(flags, args)
	if err != nil {
		return "", "", false, err
	}
	if len(words) > 1 {
		return "", "", false, fmt.Errorf("usage: %s %s NAME | --all [--hub DIR]", t.Prog, verb)
	}
	if len(words) == 1 {
		name = words[0]
	}
	return name, *hubFlag, *allFlag, nil
}

// UnlinkCounts is unlink's summary.
type UnlinkCounts struct {
	Unlinked int `json:"unlinked"`
	Kept     int `json:"kept"`
}

// UnlinkProjects removes the hub link of one manifest entry (name) or of
// every one (all). Only a symlink to the entry's own directory is removed;
// any other link or a file is kept and reported, and a name with nothing in
// the hub is passed over silently. Hub entries the manifest does not name
// are never looked at. The manifest is not changed.
func (t *Tool) UnlinkProjects(hub, name string, all bool) (UnlinkCounts, error) {
	var n UnlinkCounts
	if all == (name != "") {
		return n, fmt.Errorf("usage: %s unlink NAME | --all [--hub DIR]", t.Prog)
	}
	if hub == NoHub {
		return n, errors.New("unlink needs a hub directory, not --hub none")
	}
	hub, err := filepath.Abs(t.expand(hub))
	if err != nil {
		return n, err
	}
	entries, err := t.Manifest()
	if err != nil {
		return n, err
	}
	if !all {
		if entries, err = t.pick(entries, []string{name}); err != nil {
			return n, err
		}
	}
	for _, e := range entries {
		path := filepath.Join(hub, e.Name)
		fi, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return n, err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.say("keep: %s exists (not a link)", t.tilde(path))
			n.Kept++
			continue
		}
		target, err := os.Readlink(path)
		if err != nil {
			return n, err
		}
		if dir := t.Dir(e); target != dir && !samePath(target, dir) {
			t.say("keep: %s -> %s (not %s's directory)", t.tilde(path), target, e.Name)
			n.Kept++
			continue
		}
		if err := os.Remove(path); err != nil {
			return n, err
		}
		t.say("unlink: %s -> %s", t.tilde(path), target)
		n.Unlinked++
	}
	t.say("unlinked %d, kept %d", n.Unlinked, n.Kept)
	return n, nil
}

// Unlink is the inline command: unlink NAME | --all [--hub DIR].
func (t *Tool) Unlink(args []string) error {
	name, hub, all, err := t.hubArgs("unlink", args)
	if err != nil {
		return err
	}
	_, err = t.UnlinkProjects(hub, name, all)
	return err
}
