package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"ofam.dev/oden"
)

// Status is o-family's read-only report (moved here): every manifest entry,
// or the directories given, with tracked and changed counts, branch and
// upstream. It never stages, fetches or writes. Exit 1 when a directory is
// unavailable (missing, not a repository, an inspection error), after the
// report is printed.
func (t *Tool) Status(ctx context.Context, args []string) error {
	var dirty, clean, asJSON bool
	f := flag.NewFlagSet(t.Prog+" status", flag.ContinueOnError)
	f.SetOutput(t.Stderr)
	f.BoolVar(&dirty, "dirty", false, "show only dirty repositories")
	f.BoolVar(&dirty, "d", false, "alias for --dirty")
	f.BoolVar(&clean, "clean", false, "show only clean repositories")
	f.BoolVar(&clean, "c", false, "alias for --clean")
	f.BoolVar(&asJSON, "json", false, "print the report as JSON")
	f.BoolVar(&asJSON, "j", false, "alias for --json")
	f.Usage = func() {
		fmt.Fprintf(t.Stderr, "usage: %s status [OPTIONS] [DIR...]\n\nWith no directories, every repository in %s.\n\n  -d, --dirty  show only dirty repositories\n  -c, --clean  show only clean repositories\n  -j, --json   the report as JSON\n  -h, --help   show this usage\n\nFlags may appear before or after directories; -- ends them. Unavailable directories show under either filter.\n", t.Prog, t.tilde(t.ManifestPath()))
	}
	var dirs []string
	for len(args) > 0 {
		if args[0] == "--" {
			dirs = append(dirs, args[1:]...)
			break
		}
		if !strings.HasPrefix(args[0], "-") || args[0] == "-" {
			dirs = append(dirs, args[0])
			args = args[1:]
			continue
		}
		if err := f.Parse(args[:1]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return &oden.ExitError{Code: 2}
		}
		args = args[1:]
	}
	if dirty && clean {
		return &oden.ExitError{Code: 2, Err: errors.New("--dirty and --clean are mutually exclusive")}
	}
	repos, err := t.statusRepos(dirs)
	if err != nil {
		return err
	}
	report := collect(ctx, repos)
	if dirty || clean {
		filtered := report.Repos[:0]
		for _, repo := range report.Repos {
			if repo.Error != "" || dirty && repo.Status == "dirty" || clean && repo.Status == "clean" {
				filtered = append(filtered, repo)
			}
		}
		report.Repos = filtered
	}
	if asJSON {
		enc := json.NewEncoder(t.Stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(report)
	} else {
		err = printReport(t.Stdout, report, t.Home)
	}
	if err != nil {
		return err
	}
	if report.Summary.Errors != 0 {
		return &oden.ExitError{Code: 1}
	}
	return nil
}

// statusRepos are the directories a report inspects: dirs when given,
// else the manifest's entries; canonical paths, each once, by name.
func (t *Tool) statusRepos(dirs []string) ([]Repo, error) {
	var repos []Repo
	if len(dirs) > 0 {
		for _, dir := range dirs {
			path := t.expand(dir)
			repos = append(repos, Repo{Name: filepath.Base(filepath.Clean(path)), Path: path})
		}
	} else {
		entries, err := t.Manifest()
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			repos = append(repos, Repo{Name: e.Name, Path: t.Dir(e)})
		}
	}
	seen := map[string]bool{}
	out := make([]Repo, 0, len(repos))
	for _, repo := range repos {
		path, err := filepath.Abs(repo.Path)
		if err != nil {
			return nil, err
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		repo.Path = path
		out = append(out, repo)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func printReport(out io.Writer, report Report, home string) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tSTATUS\tTRACKED\tCHANGED\tSTAGED\tUNSTAGED\tNEW\tBRANCH\tDIRECTORY")
	for _, repo := range report.Repos {
		path := repo.Path
		if path == home {
			path = "~"
		} else if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = "~/" + rel
		}
		if repo.Error != "" {
			fmt.Fprintf(w, "%s\t%s\t-\t-\t-\t-\t-\t-\t%s\n", repo.Name, repo.Status, path)
			fmt.Fprintf(w, "  %s\n", repo.Error)
			continue
		}
		state := repo.Status
		if repo.Conflicts > 0 {
			state = fmt.Sprintf("conflict(%d)", repo.Conflicts)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n", repo.Name, state, repo.Tracked, repo.Changed, repo.Staged, repo.Unstaged, repo.Untracked, repo.Branch, path)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	s := report.Summary
	_, err := fmt.Fprintf(out, "\n%d directories: %d dirty, %d clean, %d unavailable. Showing %d.\n%d tracked files; %d changed paths (%d staged, %d unstaged, %d new, %d conflicts).\n", s.Total, s.Dirty, s.Clean, s.Errors, len(report.Repos), s.Tracked, s.Changed, s.Staged, s.Unstaged, s.Untracked, s.Conflicts)
	return err
}
