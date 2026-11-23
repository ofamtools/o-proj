package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"ofam.dev/ochazuke/cli"
	"ofam.dev/ochazuke/core"
	"ofam.dev/oden"
)

// Version is o-proj's reported version.
const Version = "0.2.0"

// Namespace is the singular namespace o-proj declares.
const Namespace = "proj"

// KindProject and KindTemplate are the extension schema kinds.
const (
	KindProject  = "proj.project"
	KindTemplate = "proj.template"
)

func (t *Tool) app(args []string) *oden.App {
	info := cli.AppInfo{
		Name:         "o-proj",
		Version:      Version,
		Summary:      "Manage family repositories, manifest, hub links, and git status",
		Modes:        func() []core.Mode { return tuiModes },
		Bindings:     func() []core.Binding { return defaultBindings() },
		Specs:        func() []core.CommandSpec { return commandSpecs() },
		ExportConfig: t.exportConfig,
		RunTUI: func(_ context.Context, tuiArgs []string) error {
			return t.runTUI(tuiArgs)
		},
		RunSmoke: func(out io.Writer, dump bool) error {
			return runSmoke(out, dump)
		},
		Manifest: extManifest(),
		Capabilities: &oden.Capabilities{
			Name:         "o-proj",
			Version:      Version,
			Summary:      "Manage family repositories, manifest, hub links, and git status",
			Capabilities: []string{"query", "catalog", "tui"},
		},
		Query: t.queryExt,
	}

	app := cli.NewApp(info, args,
		oden.WithPath(""),
		oden.WithStdio(os.Stdin, t.Stdout, t.Stderr),
		oden.WithEnv(t.Env),
	)

	app.Add(
		oden.Command{
			Name:    "new",
			Summary: "create ROOT/NAME, git init it, verify, commit, record it in the manifest, link it, ask mtm for a work project",
			Usage:   "NAME [--root DIR] [--template T] [--existing] [--github] [--private|--public] [--license mit|none] [--family|-o] [--hub DIR|none] [--no-mtm]",
			Handle:  func(c *oden.Ctx) error { return t.New(c.Args) },
		},
		oden.Command{
			Name:    "adopt",
			Summary: "record an existing repository (PATH) in the manifest and link it; its files are not touched",
			Usage:   "PATH [--name NAME] [--hub DIR|none]",
			Handle:  func(c *oden.Ctx) error { return t.Adopt(c.Args) },
		},
		oden.Command{
			Name:    "list",
			Aliases: []string{"ls"},
			Summary: "the manifest's repositories (JSON; --table for people)",
			Usage:   "[--table|-t]",
			Handle:  func(c *oden.Ctx) error { return t.List(c.Args) },
		},
		oden.Command{
			Name:    "show",
			Summary: "show one project's manifest entry, hub link, license, and git status",
			Usage:   "NAME [--json|--format json|table]",
			Handle:  t.cmdShow,
		},
		oden.Command{
			Name:    "link",
			Summary: "link one repository (NAME) or every one (--all) from the hub, ~/hub/o/NAME -> DIR",
			Usage:   "NAME | --all [--hub DIR]",
			Handle:  t.cmdLink,
		},
		oden.Command{
			Name:    "unlink",
			Summary: "remove the hub link of one repository (NAME) or of every one (--all)",
			Usage:   "NAME | --all [--hub DIR]",
			Handle:  t.cmdUnlink,
		},
		oden.Command{
			Name:    "clone",
			Summary: "clone the manifest's repositories missing on this machine, then link them",
			Usage:   "[NAME...] [--hub DIR|none] [--dry-run|-n]",
			Handle:  t.cmdClone,
		},
		oden.Command{
			Name:    "sync",
			Summary: "clone missing repositories, recreate links, fetch and report dirty/behind; --pull fast-forwards clean ones",
			Usage:   "[NAME...] [--pull|-p] [--hub DIR|none] [--dry-run|-n]",
			Handle:  t.cmdSync,
		},
		oden.Command{
			Name:    "status",
			Summary: "read-only git status of the manifest's repositories (or DIRs): dirty, clean, unavailable",
			Usage:   "[DIR...] [--dirty|-d] [--clean|-c] [--json|-j]",
			Handle:  func(c *oden.Ctx) error { return t.Status(c.Context, c.Args) },
		},
		oden.Command{
			Name:    "report",
			Summary: "markdown or structured status report for one project or the whole manifest",
			Usage:   "[NAME] [--dirty] [--clean] [--markdown] [--json|--format json|table]",
			Handle:  t.cmdReport,
		},
		t.manifestCommand(),
		t.hubCommand(),
		oden.Command{
			Name:    "seed",
			Summary: "import repositories into the manifest from hub links, o-inventory's repos.json and an o-proj list file",
			Usage:   "[--hub DIR|none] [--inventory FILE|none] [--mtm-json FILE] [--dry-run|-n]",
			Handle:  func(c *oden.Ctx) error { return t.Seed(c.Args) },
		},
		oden.Command{
			Name:    "check",
			Summary: "refuse a directory whose git toplevel is $HOME or above the project root",
			Usage:   "[DIR] [--root DIR]",
			Handle:  func(c *oden.Ctx) error { return t.Check(c.Args) },
		},
		oden.Command{
			Name:    "templates",
			Summary: "the template catalog: name, kind (dir|tar.gz), path; .tmpl files render with Go text/template",
			Handle:  func(c *oden.Ctx) error { return t.ListTemplates(c.Args) },
		},
		oden.Command{
			Name:    "tui",
			Summary: "open the interactive Ochazuke v2 project browser",
			Usage:   "[--all]",
			Handle:  func(c *oden.Ctx) error { return t.runTUI(c.Args) },
		},
	)

	return app
}

// projectCandidates builds oden.Candidate items from the manifest for c.Resolve.
func (t *Tool) projectCandidates() ([]oden.Candidate, error) {
	entries, err := t.Manifest()
	if err != nil {
		return nil, err
	}
	cands := make([]oden.Candidate, 0, len(entries))
	for _, e := range entries {
		var badges []string
		if e.Origin != "" {
			badges = append(badges, e.Origin)
		}
		if e.Branch != "" {
			badges = append(badges, e.Branch)
		}
		if e.Archived {
			badges = append(badges, "archived")
		}
		cands = append(cands, oden.Candidate{
			ID:       e.Name,
			Title:    e.Name,
			Subtitle: e.Path,
			Badges:   badges,
			Data:     e,
		})
	}
	return cands, nil
}

// resolveProject resolves a project name, prefix, or interactive selection via c.Resolve.
func (t *Tool) resolveProject(c *oden.Ctx, query string) (Entry, error) {
	cands, err := t.projectCandidates()
	if err != nil {
		return Entry{}, err
	}
	// Exact match short-circuit avoids ambiguity with prefixes.
	for _, cand := range cands {
		if cand.ID == query {
			return cand.Data.(Entry), nil
		}
	}
	picked, err := c.Resolve(query, cands, "project")
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return Entry{}, fmt.Errorf("no repository %q in %s; see %s list", query, t.tilde(t.ManifestPath()), t.Prog)
		}
		return Entry{}, err
	}
	return picked.Data.(Entry), nil
}

func (t *Tool) cmdShow(c *oden.Ctx) error {
	p, err := c.Parse(nil)
	if err != nil {
		return err
	}
	entry, err := t.resolveProject(c, p.Arg(0, ""))
	if err != nil {
		return err
	}
	dir := t.Dir(entry)
	st := inspect(c.Context, Repo{Name: entry.Name, Path: dir})
	lic := licenseOf(dir)
	lnk := t.linkedIn(t.DefaultHub(), entry.Name, dir)
	if p.Format() == "json" {
		return c.WriteJSON(map[string]any{
			"name":     entry.Name,
			"path":     entry.Path,
			"dir":      dir,
			"url":      entry.URL,
			"branch":   entry.Branch,
			"origin":   entry.Origin,
			"archived": entry.Archived,
			"license":  lic,
			"linked":   lnk,
			"status":   st,
		})
	}
	rows := [][]string{
		{"name", entry.Name},
		{"path", entry.Path},
		{"dir", dir},
		{"url", entry.URL},
		{"branch", firstNonEmpty(st.Branch, entry.Branch)},
		{"origin", entry.Origin},
		{"license", lic},
		{"linked", lnk},
		{"archived", fmt.Sprintf("%v", entry.Archived)},
		{"status", formatStatusBadge(st)},
	}
	return c.WriteTable([]string{"field", "value"}, rows)
}

func (t *Tool) cmdLink(c *oden.Ctx) error {
	name, hub, all, err := t.hubArgs("link", c.Args)
	if err != nil {
		return err
	}
	if !all && name != "" {
		entry, err := t.resolveProject(c, name)
		if err != nil {
			return err
		}
		name = entry.Name
	}
	_, err = t.LinkProjects(hub, name, all)
	return err
}

func (t *Tool) cmdUnlink(c *oden.Ctx) error {
	name, hub, all, err := t.hubArgs("unlink", c.Args)
	if err != nil {
		return err
	}
	if !all && name != "" {
		entry, err := t.resolveProject(c, name)
		if err != nil {
			return err
		}
		name = entry.Name
	}
	_, err = t.UnlinkProjects(hub, name, all)
	return err
}

func (t *Tool) cmdClone(c *oden.Ctx) error {
	names, opts, err := t.syncArgs("clone", c.Args, false)
	if err != nil {
		return err
	}
	resolved, err := t.resolveNames(c, names)
	if err != nil {
		return err
	}
	_, err = t.SyncEntries(c.Context, resolved, opts)
	return err
}

func (t *Tool) cmdSync(c *oden.Ctx) error {
	names, opts, err := t.syncArgs("sync", c.Args, true)
	if err != nil {
		return err
	}
	resolved, err := t.resolveNames(c, names)
	if err != nil {
		return err
	}
	_, err = t.SyncEntries(c.Context, resolved, opts)
	return err
}

func (t *Tool) resolveNames(c *oden.Ctx, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		e, err := t.resolveProject(c, n)
		if err != nil {
			return nil, err
		}
		out = append(out, e.Name)
	}
	return out, nil
}

func (t *Tool) cmdReport(c *oden.Ctx) error {
	params := []oden.Parameter{
		{Name: "dirty", Short: "d", Type: "bool", Description: "Include only dirty repositories"},
		{Name: "clean", Short: "c", Type: "bool", Description: "Include only clean repositories"},
		{Name: "markdown", Short: "m", Type: "bool", Description: "Render report as Markdown"},
	}
	p, err := c.Parse(params)
	if err != nil {
		return err
	}
	dirty, clean, asMD := p.Bool("dirty"), p.Bool("clean"), p.Bool("markdown")
	if dirty && clean {
		return &oden.ExitError{Code: 2, Err: fmt.Errorf("--dirty and --clean are mutually exclusive")}
	}
	pos := p.Positional()
	if len(pos) == 1 {
		entry, err := t.resolveProject(c, pos[0])
		if err != nil {
			return err
		}
		dir := t.Dir(entry)
		st := inspect(c.Context, Repo{Name: entry.Name, Path: dir})
		if p.Format() == "json" {
			return c.WriteJSON(map[string]any{"entry": entry, "dir": dir, "status": st})
		}
		md := t.projectMarkdown(entry, &st)
		_, err = io.WriteString(c.Stdout, md+"\n")
		return err
	}
	repos, err := t.statusRepos(nil)
	if err != nil {
		return err
	}
	rep := collect(c.Context, repos)
	if dirty || clean {
		filtered := rep.Repos[:0]
		for _, r := range rep.Repos {
			if r.Error != "" || (dirty && r.Status == "dirty") || (clean && r.Status == "clean") {
				filtered = append(filtered, r)
			}
		}
		rep.Repos = filtered
	}
	if p.Format() == "json" {
		return c.WriteJSON(rep)
	}
	if asMD {
		_, err := io.WriteString(c.Stdout, formatReportMarkdown(rep, t.Home)+"\n")
		return err
	}
	rows := make([][]string, 0, len(rep.Repos))
	for _, r := range rep.Repos {
		rows = append(rows, []string{
			r.Name,
			formatStatusBadge(r),
			r.Branch,
			fmt.Sprintf("%d", r.Tracked),
			fmt.Sprintf("%d", r.Changed),
			t.tilde(r.Path),
		})
	}
	return c.WriteTable([]string{"name", "status", "branch", "tracked", "changed", "path"}, rows)
}

func (t *Tool) manifestCommand() oden.Command {
	listParams := []oden.Parameter{
		{Name: "archived", Type: "bool", Description: "Include archived repositories"},
	}
	return oden.Command{
		Name:    "manifest",
		Summary: "inspect and edit the repository manifest",
		Children: []oden.Command{
			{
				Name:    "path",
				Summary: "print the manifest file path",
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(nil)
					if err != nil {
						return err
					}
					mp := t.ManifestPath()
					if p.Format() == "json" {
						return c.WriteJSON(map[string]string{"path": mp, "tilde": t.tilde(mp)})
					}
					return c.Println(mp)
				},
			},
			{
				Name:       "list",
				Summary:    "list manifest entries",
				Usage:      "[--archived] [--json|--format json|table]",
				Parameters: listParams,
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(listParams)
					if err != nil {
						return err
					}
					entries, err := t.Manifest()
					if err != nil {
						return err
					}
					showArchived := p.Bool("archived")
					filtered := make([]Row, 0, len(entries))
					for _, r := range t.rows(entries) {
						if showArchived || !r.Archived {
							filtered = append(filtered, r)
						}
					}
					if p.Format() == "json" {
						return c.WriteJSON(filtered)
					}
					rows := make([][]string, 0, len(filtered))
					for _, r := range filtered {
						rows = append(rows, []string{r.Name, r.Origin, r.Branch, r.Path, licenseOf(r.Dir), t.linkedIn(t.DefaultHub(), r.Name, r.Dir)})
					}
					return c.WriteTable([]string{"name", "origin", "branch", "path", "license", "linked"}, rows)
				},
			},
			{
				Name:    "archive",
				Summary: "hide a project from default lists (manifest only)",
				Usage:   "NAME",
				Handle: func(c *oden.Ctx) error {
					return t.setArchived(c, true)
				},
			},
			{
				Name:    "restore",
				Summary: "unarchive a project in the manifest",
				Usage:   "NAME",
				Handle: func(c *oden.Ctx) error {
					return t.setArchived(c, false)
				},
			},
			{
				Name:    "delete",
				Summary: "remove a project entry from the manifest (keeps directory and hub link)",
				Usage:   "NAME",
				Handle: func(c *oden.Ctx) error {
					p, err := c.Parse(nil)
					if err != nil {
						return err
					}
					entry, err := t.resolveProject(c, p.Arg(0, ""))
					if err != nil {
						return err
					}
					if err := t.deleteEntry(entry.Name); err != nil {
						return err
					}
					if p.Format() == "json" {
						return c.WriteJSON(map[string]any{"deleted": entry.Name})
					}
					return c.Printf("removed %s from %s\n", entry.Name, t.tilde(t.ManifestPath()))
				},
			},
		},
	}
}

func (t *Tool) setArchived(c *oden.Ctx, archived bool) error {
	p, err := c.Parse(nil)
	if err != nil {
		return err
	}
	entry, err := t.resolveProject(c, p.Arg(0, ""))
	if err != nil {
		return err
	}
	updated, err := t.updateEntry(entry.Name, func(e *Entry) { e.Archived = archived })
	if err != nil {
		return err
	}
	if p.Format() == "json" {
		return c.WriteJSON(updated)
	}
	state := "archived"
	if !archived {
		state = "restored"
	}
	return c.Printf("%s: %s\n", updated.Name, state)
}

func (t *Tool) updateEntry(name string, mutate func(*Entry)) (Entry, error) {
	entries, err := t.Manifest()
	if err != nil {
		return Entry{}, err
	}
	idx := -1
	for i := range entries {
		if entries[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Entry{}, fmt.Errorf("no repository %q in %s; see %s list", name, t.tilde(t.ManifestPath()), t.Prog)
	}
	mutate(&entries[idx])
	if err := t.SaveManifest(entries); err != nil {
		return Entry{}, err
	}
	return entries[idx], nil
}

func (t *Tool) deleteEntry(name string) error {
	entries, err := t.Manifest()
	if err != nil {
		return err
	}
	kept := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Name != name {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(entries) {
		return fmt.Errorf("no repository %q in %s; see %s list", name, t.tilde(t.ManifestPath()), t.Prog)
	}
	return t.SaveManifest(kept)
}

func (t *Tool) hubCommand() oden.Command {
	return oden.Command{
		Name:    "hub",
		Summary: "inspect and manage hub symlinks",
		Children: []oden.Command{
			{
				Name:    "status",
				Summary: "show hub link state for every manifest entry",
				Usage:   "[--hub DIR] [--json|--format json|table]",
				Parameters: []oden.Parameter{
					{Name: "hub", Type: "string", Description: "Hub directory (default $O_PROJ_HUB or ~/hub/o)"},
				},
				Handle: func(c *oden.Ctx) error {
					params := []oden.Parameter{{Name: "hub", Type: "string"}}
					p, err := c.Parse(params)
					if err != nil {
						return err
					}
					hub := p.String("hub")
					if hub == "" {
						hub = t.DefaultHub()
					}
					entries, err := t.Manifest()
					if err != nil {
						return err
					}
					type hubRow struct {
						Name   string `json:"name"`
						Path   string `json:"path"`
						Linked string `json:"linked"`
					}
					var items []hubRow
					var rows [][]string
					for _, e := range entries {
						lnk := t.linkedIn(hub, e.Name, t.Dir(e))
						items = append(items, hubRow{Name: e.Name, Path: e.Path, Linked: lnk})
						rows = append(rows, []string{e.Name, lnk, e.Path})
					}
					if p.Format() == "json" {
						return c.WriteJSON(items)
					}
					return c.WriteTable([]string{"name", "linked", "path"}, rows)
				},
			},
			{
				Name:    "link",
				Summary: "link one repository (NAME) or every one (--all) from the hub",
				Usage:   "NAME | --all [--hub DIR]",
				Handle:  t.cmdLink,
			},
			{
				Name:    "unlink",
				Summary: "remove the hub link of one repository (NAME) or every one (--all)",
				Usage:   "NAME | --all [--hub DIR]",
				Handle:  t.cmdUnlink,
			},
		},
	}
}

func (t *Tool) exportConfig(jsonl bool) ([]byte, error) {
	cfg := map[string]any{
		"manifest":      t.tilde(t.ManifestPath()),
		"hub":           t.tilde(t.expand(t.DefaultHub())),
		"root":          t.tilde(t.DefaultRoot()),
		"templates":     t.tilde(t.TemplateDir()),
		"github_owner":  t.GitHubOwner(),
		"module_prefix": t.ModulePrefix(),
		"settings":      defaultSettings,
	}
	if jsonl {
		var b strings.Builder
		for _, k := range []string{"manifest", "hub", "root", "templates", "github_owner", "module_prefix"} {
			line, err := json.Marshal(map[string]any{"key": k, "value": cfg[k]})
			if err != nil {
				return nil, err
			}
			b.Write(line)
			b.WriteByte('\n')
		}
		return []byte(b.String()), nil
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func extManifest() *oden.ExtManifest {
	return &oden.ExtManifest{
		Name:    "o-proj",
		Version: Version,
		Schemas: []oden.ExtSchema{
			{
				Kind:           KindProject,
				Title:          "Project",
				Plural:         "Projects",
				DefaultColumns: []string{"name", "origin", "path", "license", "linked"},
				SummaryField:   "name",
				UniqueFields:   []string{"name"},
				Fields: []oden.ExtField{
					{Name: "name", Type: "string", Required: true, Description: "Repository name in the manifest and hub"},
					{Name: "path", Type: "path", Required: true, Description: "Repository path with ~ for home"},
					{Name: "dir", Type: "path", Description: "Expanded repository directory on this machine"},
					{Name: "url", Type: "string", Description: "Origin remote URL"},
					{Name: "branch", Type: "string", Description: "Recorded default branch"},
					{Name: "origin", Type: "string", Enum: []string{OriginNew, OriginAdopted}, Description: "How the project entered the manifest"},
					{Name: "license", Type: "string", Enum: []string{"mit", "other", "none", ""}, Description: "Detected license in the directory"},
					{Name: "linked", Type: "string", Enum: []string{"yes", "no", "other", ""}, Description: "Whether the hub links this repository"},
					{Name: "archived", Type: "bool", Description: "Hidden from default project lists"},
				},
			},
			{
				Kind:           KindTemplate,
				Title:          "Template",
				Plural:         "Templates",
				DefaultColumns: []string{"name", "kind", "path"},
				SummaryField:   "name",
				UniqueFields:   []string{"name"},
				Fields: []oden.ExtField{
					{Name: "name", Type: "string", Required: true, Description: "Template name"},
					{Name: "kind", Type: "string", Enum: []string{"dir", "tar.gz"}, Description: "Template format"},
					{Name: "path", Type: "path", Description: "Template path"},
				},
			},
		},
		Commands: []oden.ExtCommand{
			{ID: "proj.project.list", Description: "List manifest projects", Tags: []string{"read"}},
			{ID: "proj.project.show", Description: "Show one project with git status", Tags: []string{"read"}},
			{ID: "proj.project.new", Description: "Create a new project repository", Tags: []string{"write"}},
			{ID: "proj.project.adopt", Description: "Adopt an existing git repository into the manifest", Tags: []string{"write"}},
			{ID: "proj.project.link", Description: "Link project(s) into the hub", Tags: []string{"write"}},
			{ID: "proj.project.unlink", Description: "Unlink project(s) from the hub", Tags: []string{"write"}},
			{ID: "proj.project.clone", Description: "Clone missing repositories from the manifest", Tags: []string{"write"}},
			{ID: "proj.project.sync", Description: "Clone, link, fetch, and optionally fast-forward projects", Tags: []string{"write"}},
			{ID: "proj.project.status", Description: "Read-only git status across manifest repositories", Tags: []string{"read"}},
			{ID: "proj.project.report", Description: "Generate a Markdown or table status report", Tags: []string{"read"}},
			{ID: "proj.project.seed", Description: "Seed manifest from hub links, o-inventory, or mtm export", Tags: []string{"write"}},
			{ID: "proj.project.check", Description: "Verify a directory is not inside $HOME or above project root", Tags: []string{"read"}},
		},
	}
}

func (t *Tool) queryExt(ctx context.Context, kind string, args []string) (oden.ExtQueryResult, error) {
	if kind == "" {
		kind = KindProject
	}
	var includeArchived bool
	var terms []string
	for _, a := range args {
		if a == "--archived" {
			includeArchived = true
			continue
		}
		terms = append(terms, strings.ToLower(a))
	}
	matches := func(haystack string) bool {
		if len(terms) == 0 {
			return true
		}
		low := strings.ToLower(haystack)
		for _, term := range terms {
			if !strings.Contains(low, term) {
				return false
			}
		}
		return true
	}

	switch kind {
	case KindTemplate, "template":
		tpls, err := t.Templates()
		if err != nil {
			return oden.ExtQueryResult{}, err
		}
		items := make([]oden.ExtRecord, 0, len(tpls))
		for _, tpl := range tpls {
			tp := t.tilde(tpl.Path)
			if !matches(tpl.Name + " " + tpl.Kind + " " + tp) {
				continue
			}
			items = append(items, oden.ExtRecord{
				Kind: KindTemplate,
				ID:   tpl.Name,
				Fields: map[string]any{
					"name": tpl.Name,
					"kind": tpl.Kind,
					"path": tp,
				},
			})
		}
		return oden.ExtQueryResult{Items: items, Total: len(items)}, nil

	case KindProject, "project":
		entries, err := t.Manifest()
		if err != nil {
			return oden.ExtQueryResult{}, err
		}
		items := make([]oden.ExtRecord, 0, len(entries))
		for _, e := range entries {
			if e.Archived && !includeArchived {
				continue
			}
			dir := t.Dir(e)
			lic := licenseOf(dir)
			lnk := t.linkedIn(t.DefaultHub(), e.Name, dir)
			hay := strings.Join([]string{e.Name, e.Path, e.URL, e.Branch, e.Origin, lic, lnk}, " ")
			if !matches(hay) {
				continue
			}
			archiveStatus := "active"
			if e.Archived {
				archiveStatus = "archived"
			}
			items = append(items, oden.ExtRecord{
				Kind:          KindProject,
				ID:            e.Name,
				ArchiveStatus: archiveStatus,
				Fields: map[string]any{
					"name":     e.Name,
					"path":     e.Path,
					"dir":      dir,
					"url":      e.URL,
					"branch":   e.Branch,
					"origin":   e.Origin,
					"license":  lic,
					"linked":   lnk,
					"archived": e.Archived,
				},
				ReadOnly: map[string]bool{"dir": true, "license": true, "linked": true},
			})
		}
		return oden.ExtQueryResult{Items: items, Total: len(items)}, nil

	default:
		return oden.ExtQueryResult{}, fmt.Errorf("unknown query kind %q (supported: %s, %s)", kind, KindProject, KindTemplate)
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func formatStatusBadge(st RepoStatus) string {
	if st.Error != "" {
		return st.Status
	}
	if st.Conflicts > 0 {
		return fmt.Sprintf("conflict(%d)", st.Conflicts)
	}
	if st.Status == "dirty" {
		return fmt.Sprintf("dirty(%d)", st.Changed)
	}
	if st.Status == "" {
		return "unknown"
	}
	return st.Status
}

func formatReportMarkdown(rep Report, home string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Project Status Report\n\n")
	s := rep.Summary
	fmt.Fprintf(&b, "- **Repositories:** %d (%d clean, %d dirty, %d unavailable)\n", s.Total, s.Clean, s.Dirty, s.Errors)
	fmt.Fprintf(&b, "- **Files:** %d tracked, %d changed (%d staged, %d unstaged, %d untracked, %d conflicts)\n\n",
		s.Tracked, s.Changed, s.Staged, s.Unstaged, s.Untracked, s.Conflicts)
	fmt.Fprintf(&b, "| Repo | Status | Branch | Changed | Path |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | ---: | --- |\n")
	for _, r := range rep.Repos {
		p := r.Path
		if p == home {
			p = "~"
		} else if rel, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			p = "~/" + rel
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | `%s` |\n", r.Name, formatStatusBadge(r), r.Branch, r.Changed, p)
	}
	return strings.TrimRight(b.String(), "\n")
}
