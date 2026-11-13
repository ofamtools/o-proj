package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ofam.dev/oden"
)

// Seed imports repositories into the manifest once, from what recorded
// them before the manifest existed: the hub's links, o-inventory's
// repos.json, and a saved o-proj list from the time o-proj wrote mtm's
// store (--mtm-json; only rows with origin o-proj). It adds missing names
// only and never rewrites an entry. A name's directory comes from the first
// source that has it, in this order: the manifest, the hub link, repos.json,
// the mtm export. Sources that disagree on a name's directory are printed
// as conflict lines and nothing is written (exit 1); adopt the right
// directory first, then seed again.
func (t *Tool) Seed(args []string) error {
	flags := flag.NewFlagSet(t.Prog+" seed", flag.ContinueOnError)
	flags.SetOutput(t.Stderr)
	var o SeedOptions
	flags.StringVar(&o.Hub, "hub", t.DefaultHub(), "the hub directory whose links are read; none skips")
	flags.StringVar(&o.Inventory, "inventory", t.DefaultInventory(), "o-inventory's repos.json; none skips")
	flags.StringVar(&o.MtmJSON, "mtm-json", "", "a saved o-proj list (the version that read mtm's store); rows with origin o-proj are read")
	flags.BoolVar(&o.DryRun, "dry-run", false, "print what would be added; write nothing")
	flags.BoolVar(&o.DryRun, "n", false, "alias for --dry-run")
	words, err := parse(flags, args)
	if err != nil {
		return err
	}
	if len(words) > 0 {
		return fmt.Errorf("usage: %s seed [--hub DIR|none] [--inventory FILE|none] [--mtm-json FILE] [--dry-run]", t.Prog)
	}
	_, err = t.SeedEntries(o)
	return err
}

// SeedOptions are seed's sources and switch: the hub directory whose links
// are read, o-inventory's repos.json, a saved o-proj list, and a dry run.
type SeedOptions struct {
	Hub       string
	Inventory string
	MtmJSON   string
	DryRun    bool
}

// SeedResult is what seed found: the entries it added (or would add on a
// dry run) and the names whose sources disagree.
type SeedResult struct {
	Added     []Entry  `json:"added"`
	Conflicts []string `json:"conflicts,omitempty"`
	DryRun    bool     `json:"dry_run,omitempty"`
}

// DefaultInventory is o-inventory's repos.json under the config directory.
func (t *Tool) DefaultInventory() string {
	config := lookup(t.Env, "XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(t.Home, ".config")
	}
	return filepath.Join(config, "o-inventory", "repos.json")
}

// seedSource is one place that names repositories: it adds (name, directory)
// candidates and notes the names o-proj itself made.
type seedSource struct {
	found       map[string][]seedCandidate
	madeByOProj map[string]bool
}

type seedCandidate struct{ source, dir string }

func (s *seedSource) add(source, name, dir string) {
	if name != "" && dir != "" {
		s.found[name] = append(s.found[name], seedCandidate{source, dir})
	}
}

// hubLinks reads the symlinks in the hub directory: each name's target.
func (t *Tool) seedHub(s *seedSource, hub string) error {
	dir := t.expand(hub)
	links, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, l := range links {
		if l.Type()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(filepath.Join(dir, l.Name()))
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		s.add("hub", l.Name(), target)
	}
	return nil
}

// seedInventory reads o-inventory's repos.json, when there is one.
func (t *Tool) seedInventory(s *seedSource, file string) error {
	data, err := os.ReadFile(t.expand(file))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var repos []struct{ Name, Path string }
	if err := json.Unmarshal(data, &repos); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	for _, r := range repos {
		s.add("o-inventory", r.Name, t.expand(r.Path))
	}
	return nil
}

// seedMtm reads a saved o-proj list: the rows o-proj made.
func (t *Tool) seedMtm(s *seedSource, file string) error {
	data, err := os.ReadFile(t.expand(file))
	if err != nil {
		return err
	}
	var projects []struct {
		Name        string `json:"name"`
		Origin      string `json:"origin"`
		DefaultTree string `json:"default_tree"`
		Trees       []struct {
			Path string `json:"path"`
		} `json:"trees"`
	}
	if err := json.Unmarshal(data, &projects); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	for _, p := range projects {
		if p.Origin != OriginNew {
			continue
		}
		s.madeByOProj[p.Name] = true
		dir := p.DefaultTree
		if dir == "" && len(p.Trees) > 0 {
			dir = p.Trees[0].Path
		}
		s.add("mtm", p.Name, dir)
	}
	return nil
}

// SeedEntries is Seed over its options: one line per entry added or in
// conflict, and the result as data.
func (t *Tool) SeedEntries(o SeedOptions) (SeedResult, error) {
	var result SeedResult
	src := &seedSource{found: map[string][]seedCandidate{}, madeByOProj: map[string]bool{}}
	entries, err := t.Manifest()
	if err != nil {
		return result, err
	}
	for _, e := range entries {
		src.add("manifest", e.Name, t.Dir(e))
	}
	if o.Hub != NoHub {
		if err := t.seedHub(src, o.Hub); err != nil {
			return result, err
		}
	}
	if o.Inventory != NoHub {
		if err := t.seedInventory(src, o.Inventory); err != nil {
			return result, err
		}
	}
	if o.MtmJSON != "" {
		if err := t.seedMtm(src, o.MtmJSON); err != nil {
			return result, err
		}
	}
	found, madeByOProj := src.found, src.madeByOProj

	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	var added []Entry
	conflicts := 0
	for _, name := range names {
		cands := found[name]
		chosen := cands[0]
		var sources []string
		for _, c := range cands {
			sources = append(sources, c.source)
			if dirKey(c.dir) != dirKey(chosen.dir) {
				t.say("conflict: %s: %s says %s, %s says %s", name, chosen.source, t.tilde(chosen.dir), c.source, t.tilde(c.dir))
				conflicts++
				result.Conflicts = append(result.Conflicts, name)
			}
		}
		if chosen.source == "manifest" {
			continue
		}
		origin := OriginAdopted
		if madeByOProj[name] {
			origin = OriginNew
		}
		dir := dirKey(chosen.dir)
		e := Entry{Name: name, Path: t.tilde(dir), Origin: origin}
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			e = t.describe(name, dir, origin)
		}
		added = append(added, e)
		t.say("seed: %s -> %s (%s)", name, e.Path, strings.Join(sources, ", "))
	}
	result.Added, result.DryRun = added, o.DryRun
	if conflicts > 0 {
		t.say("seed: %d conflicts; nothing written. Adopt the right directory for each (%s adopt PATH --name NAME), then seed again", conflicts, t.Prog)
		return result, &oden.ExitError{Code: 1}
	}
	if o.DryRun {
		t.say("seed: dry run: %d to add to %s", len(added), t.tilde(t.ManifestPath()))
		return result, nil
	}
	if len(added) == 0 {
		t.say("seed: nothing to add")
		return result, nil
	}
	if err := t.SaveManifest(append(entries, added...)); err != nil {
		return result, err
	}
	t.say("seed: added %d to %s", len(added), t.tilde(t.ManifestPath()))
	return result, nil
}
