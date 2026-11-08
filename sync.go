package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ofam.dev/oden"
)

// SyncOptions are clone's and sync's switches.
type SyncOptions struct {
	Hub    string // link into this hub; NoHub skips links
	Fetch  bool   // git fetch each repository with an upstream (sync)
	Pull   bool   // git pull --ff-only where clean, behind and not ahead
	DryRun bool   // report only: no clone, link, fetch or pull
}

// SyncResult is one entry's outcome.
type SyncResult struct {
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	Action   string `json:"action"` // cloned, would-clone, missing, present
	Link     string `json:"link,omitempty"`
	Status   string `json:"status,omitempty"` // clean, dirty, or a status error state
	Changed  int    `json:"changed,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Pulled   bool   `json:"pulled,omitempty"`
	Error    string `json:"error,omitempty"`
}

// syncArgs parses [NAME...] [--hub DIR|none] plus sync's own flags.
func (t *Tool) syncArgs(verb string, args []string, withSync bool) ([]string, SyncOptions, error) {
	flags := flag.NewFlagSet(t.Prog+" "+verb, flag.ContinueOnError)
	flags.SetOutput(t.Stderr)
	opts := SyncOptions{Fetch: withSync}
	flags.StringVar(&opts.Hub, "hub", t.DefaultHub(), "the hub directory to link into; none skips links")
	flags.BoolVar(&opts.DryRun, "dry-run", false, "report what would happen; clone, link, fetch and pull nothing")
	flags.BoolVar(&opts.DryRun, "n", false, "alias for --dry-run")
	if withSync {
		flags.BoolVar(&opts.Pull, "pull", false, "git pull --ff-only in clean repositories that are behind")
		flags.BoolVar(&opts.Pull, "p", false, "alias for --pull")
	}
	words, err := parse(flags, args)
	if opts.DryRun {
		opts.Fetch = false
	}
	return words, opts, err
}

// Clone is the inline command: clone the missing entries, then link them.
func (t *Tool) Clone(ctx context.Context, args []string) error {
	names, opts, err := t.syncArgs("clone", args, false)
	if err != nil {
		return err
	}
	_, err = t.SyncEntries(ctx, names, opts)
	return err
}

// Sync is the inline command: clone, link, fetch, report, optionally pull.
func (t *Tool) Sync(ctx context.Context, args []string) error {
	names, opts, err := t.syncArgs("sync", args, true)
	if err != nil {
		return err
	}
	_, err = t.SyncEntries(ctx, names, opts)
	return err
}

// SyncEntries brings this machine in line with the manifest, one line per
// entry, then a summary. It never touches a dirty repository's files,
// never pulls one that is ahead (only --ff-only) and never replaces
// anything already at a hub name. A failed clone, fetch or pull is
// reported on its line and makes the whole run exit 1 after the summary.
func (t *Tool) SyncEntries(ctx context.Context, names []string, opts SyncOptions) ([]SyncResult, error) {
	entries, err := t.Manifest()
	if err != nil {
		return nil, err
	}
	if entries, err = t.pick(entries, names); err != nil {
		return nil, err
	}
	var results []SyncResult
	failed := 0
	for _, e := range entries {
		r := t.syncEntry(ctx, e, opts)
		if r.Error != "" {
			failed++
		}
		t.say("%s", syncLine(r))
		results = append(results, r)
	}
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Action]++
		if r.Status == "dirty" {
			counts["dirty"]++
		}
		if r.Behind > 0 && !r.Pulled {
			counts["behind"]++
		}
		if r.Pulled {
			counts["pulled"]++
		}
	}
	prefix := ""
	if opts.DryRun {
		prefix = "dry run: "
	}
	t.say("%s%d repositories: %d cloned, %d to clone, %d missing, %d dirty, %d behind, %d pulled, %d failed", prefix, len(results), counts["cloned"], counts["would-clone"], counts["missing"], counts["dirty"], counts["behind"], counts["pulled"], failed)
	if failed > 0 {
		return results, &oden.ExitError{Code: 1}
	}
	return results, nil
}

func (t *Tool) syncEntry(ctx context.Context, e Entry, opts SyncOptions) SyncResult {
	dir := t.Dir(e)
	r := SyncResult{Name: e.Name, Dir: t.tilde(dir), Action: "present"}
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		switch {
		case e.URL == "":
			r.Action = "missing"
			return r
		case opts.DryRun:
			r.Action = "would-clone"
			return r
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			r.Action, r.Error = "missing", err.Error()
			return r
		}
		argv := []string{"clone", "--quiet"}
		if e.Branch != "" {
			argv = append(argv, "--branch", e.Branch)
		}
		if _, err := t.Run(filepath.Dir(dir), "git", append(argv, "--", e.URL, dir)...); err != nil {
			r.Action, r.Error = "missing", err.Error()
			return r
		}
		r.Action = "cloned"
	}
	if opts.Hub != NoHub && !opts.DryRun {
		r.Link, _ = t.linkQuiet(opts.Hub, e.Name, dir)
	}
	if opts.Fetch {
		if s := inspect(ctx, Repo{Name: e.Name, Path: dir}); s.Upstream != "" {
			if _, err := t.Run(dir, "git", "fetch", "--quiet"); err != nil {
				r.Error = err.Error()
			}
		}
	}
	s := inspect(ctx, Repo{Name: e.Name, Path: dir})
	r.Status, r.Changed, r.Upstream, r.Ahead, r.Behind = s.Status, s.Changed, s.Upstream, s.Ahead, s.Behind
	if s.Error != "" && r.Error == "" {
		r.Error = s.Error
	}
	if opts.Pull && !opts.DryRun && r.Error == "" && s.Status == "clean" && s.Behind > 0 && s.Ahead == 0 {
		if _, err := t.Run(dir, "git", "pull", "--quiet", "--ff-only"); err != nil {
			r.Error = err.Error()
		} else {
			r.Pulled, r.Behind = true, 0
		}
	}
	return r
}

// linkQuiet links like link but returns the outcome without a line of its
// own (sync prints one line per entry).
func (t *Tool) linkQuiet(hub, name, dir string) (string, error) {
	saved := t.Stdout
	t.Stdout = discard{}
	defer func() { t.Stdout = saved }()
	return t.link(hub, name, dir)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// syncLine is one entry's report line.
func syncLine(r SyncResult) string {
	parts := []string{r.Name + ":"}
	switch r.Action {
	case "cloned":
		parts = append(parts, "cloned into "+r.Dir)
	case "would-clone":
		parts = append(parts, "would clone into "+r.Dir)
	case "missing":
		parts = append(parts, "missing "+r.Dir+" (no url to clone from)")
		if r.Error != "" {
			parts[len(parts)-1] = "missing " + r.Dir + " (clone failed)"
		}
	}
	if r.Status != "" {
		state := r.Status
		if r.Status == "dirty" {
			state = fmt.Sprintf("dirty (%d changed)", r.Changed)
		}
		parts = append(parts, state)
	}
	if r.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("ahead %d", r.Ahead))
	}
	if r.Behind > 0 {
		parts = append(parts, fmt.Sprintf("behind %d", r.Behind))
	}
	if r.Pulled {
		parts = append(parts, "pulled")
	}
	if r.Link == kept {
		parts = append(parts, "hub link kept (something else is there)")
	}
	if r.Error != "" {
		parts = append(parts, "error: "+r.Error)
	}
	return strings.Join(parts, " ")
}
