package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// The data side of o-proj is an ochazuke runtime with two nouns, project
// and template, behind ochazuke/cli (mounted in Main): o-proj project VERB [ARG...]
// [--ARG VALUE] runs a verb and prints its result as JSON, and o-proj
// catalog describes the nouns for a host (nabe) that mounts the bin. The
// rows of project list are the manifest's repositories, {id: name, label:
// name, data: row, cells}; template list's are the template catalog, {id:
// name, label: name, data: template, cells} (columns in table.go). A
// --target no list shows yet is revealed by loading them. There is no TUI: the runtime has no page and nothing renders it.

// projectRow is one element of project list.
type projectRow struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Data  Row      `json:"data"`
	Cells []string `json:"cells,omitempty"`
}

// templateRow is one element of template list.
type templateRow struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Data  Template `json:"data"`
	Cells []string `json:"cells,omitempty"`
}

// Namespace is the singular namespace o-proj declares (lexicon plan): its
// operations are proj.<noun>.<verb> and a host mounts its catalog under it.
const Namespace = "proj"

func init() {
	if err := ochazuke.DeclareNamespace(Namespace); err != nil {
		panic(err)
	}
}

// shell is the NOUN VERB shell over a runtime holding the project noun.
func (t *Tool) shell() (*ocli.Shell, error) {
	r := ochazuke.NewRuntime()
	list, err := r.AddList("projects", nil)
	if err != nil {
		return nil, err
	}
	list.SetColumns(projectColumns...)
	templates, err := r.AddList("templates", nil)
	if err != nil {
		return nil, err
	}
	templates.SetColumns(templateColumns...)
	// load reads the manifest into the list; archived rows stay out unless
	// archived is set (project list --archived), as every archive role hides
	// its things from a list. The list always holds every row, so --target
	// names an archived project too.
	load := func(archived bool) ([]projectRow, error) {
		entries, err := t.Manifest()
		if err != nil {
			return nil, err
		}
		rows := make([]projectRow, 0, len(entries))
		items := make([]ochazuke.ListItem, 0, len(entries))
		for _, row := range t.rows(entries) {
			cells := t.projectCells(row)
			if archived || !row.Archived {
				rows = append(rows, projectRow{ID: row.Name, Label: row.Name, Data: row, Cells: cells})
			}
			items = append(items, ochazuke.ListItem{ID: row.Name, Label: row.Name, Noun: "project", Data: row, Cells: cells})
		}
		return rows, list.SetItems(items)
	}
	loadTemplates := func() ([]templateRow, error) {
		catalog, err := t.Templates()
		if err != nil {
			return nil, err
		}
		rows := make([]templateRow, 0, len(catalog))
		items := make([]ochazuke.ListItem, 0, len(catalog))
		for _, tpl := range catalog {
			cells := t.templateCells(tpl)
			rows = append(rows, templateRow{ID: tpl.Name, Label: tpl.Name, Data: tpl, Cells: cells})
			items = append(items, ochazuke.ListItem{ID: tpl.Name, Label: tpl.Name, Noun: "template", Data: tpl, Cells: cells})
		}
		return rows, templates.SetItems(items)
	}
	result := func(v any, err error) ochazuke.Result {
		if err != nil {
			return ochazuke.Failed(err)
		}
		return ochazuke.Done(v)
	}
	quiet := func(f func() (any, error)) (any, error) {
		saved := t.Stdout
		t.Stdout = discard{}
		defer func() { t.Stdout = saved }()
		return f()
	}
	// o-proj has no page or keys, so the noun declines the generated key block.
	r.Define("project").Main().DefaultKeys(false).Props(append(projectProps(), ochazuke.PropSpec{Name: "status", ReadOnly: true, Subject: ochazuke.SubjectCollection,
		Parameter: ochazuke.Parameter{Type: ochazuke.PropObject, Description: "read-only git status of every repository: {repos, summary}"},
		Read: func(ochazuke.Ctx, ochazuke.Thing) (any, error) {
			repos, err := t.statusRepos(nil)
			if err != nil {
				return nil, err
			}
			return collect(context.Background(), repos), nil
		}})...).Store(t.projectStore()).Columns(projectColumns...).
		Verb("list", func(c ochazuke.Ctx) ochazuke.Result {
			all, _ := c.Args["archived"].(bool)
			return result(load(all))
		}, "the manifest's repositories: {id, label, data: {name, url, path, branch, origin, archived, dir}}; archived ones only with --archived").Rows("project").Fetch().
		Verb("new", func(c ochazuke.Ctx) ochazuke.Result { return result(t.newFromArgs(c.Args)) },
			"create ROOT/NAME, git init it, verify, commit, record it in the manifest, link it, ask mtm for a work project").
		Verb("adopt", func(c ochazuke.Ctx) ochazuke.Result {
			return result(t.AdoptPath(ochazuke.StringArg(c.Args, "path", ""), ochazuke.StringArg(c.Args, "name", ""), ochazuke.StringArg(c.Args, "hub", t.DefaultHub())))
		}, "record an existing repository (path) in the manifest and link it: {entry, outcome}").Positional("path").
		Verb("link", func(c ochazuke.Ctx) ochazuke.Result {
			all, _ := c.Args["all"].(bool)
			return result(t.LinkProjects(ochazuke.StringArg(c.Args, "hub", t.DefaultHub()), ochazuke.StringArg(c.Args, "name", ""), all))
		}, "link one repository (name) or every one (all) from the hub: {linked, kept, missing}").Positional("name").
		Verb("unlink", func(c ochazuke.Ctx) ochazuke.Result {
			all, _ := c.Args["all"].(bool)
			return result(t.UnlinkProjects(ochazuke.StringArg(c.Args, "hub", t.DefaultHub()), ochazuke.StringArg(c.Args, "name", ""), all))
		}, "remove the hub link of one repository (name) or of every one (all): {unlinked, kept}").Positional("name").
		Verb("sync", func(c ochazuke.Ctx) ochazuke.Result {
			pull, _ := c.Args["pull"].(bool)
			dry, _ := c.Args["dry_run"].(bool)
			return result(quiet(func() (any, error) {
				return t.SyncEntries(context.Background(), nil, SyncOptions{Hub: t.DefaultHub(), Fetch: !dry, Pull: pull, DryRun: dry})
			}))
		}, "clone missing repositories, link, fetch and report; pull fast-forwards clean ones: [{name, action, status, ahead, behind, ...}]").
		Verb("clone", func(c ochazuke.Ctx) ochazuke.Result {
			dry, _ := c.Args["dry_run"].(bool)
			return result(quiet(func() (any, error) {
				return t.SyncEntries(context.Background(), namesArg(c.Args), SyncOptions{Hub: ochazuke.StringArg(c.Args, "hub", t.DefaultHub()), DryRun: dry})
			}))
		}, "clone the manifest's repositories missing on this machine (all, or the names given), then link them: [{name, action, link, ...}]").Positional("name", "more").
		Verb("seed", func(c ochazuke.Ctx) ochazuke.Result {
			dry, _ := c.Args["dry_run"].(bool)
			return result(quiet(func() (any, error) {
				return t.SeedEntries(SeedOptions{Hub: ochazuke.StringArg(c.Args, "hub", t.DefaultHub()), Inventory: ochazuke.StringArg(c.Args, "inventory", t.DefaultInventory()), MtmJSON: ochazuke.StringArg(c.Args, "mtm_json", ""), DryRun: dry})
			}))
		}, "import repositories into the manifest from hub links, o-inventory's repos.json and a saved o-proj list: {added, conflicts}; sources that disagree on a directory write nothing").
		Verb("check", func(c ochazuke.Ctx) ochazuke.Result {
			dir, top, err := t.guard(ochazuke.StringArg(c.Args, "dir", "."), t.DefaultRoot())
			if err != nil {
				return ochazuke.Failed(err)
			}
			return ochazuke.Done(map[string]string{"dir": dir, "toplevel": top})
		}, "refuse a directory whose git toplevel is $HOME or above the project root").Fetch().
		Verb("open", func(c ochazuke.Ctx) ochazuke.Result {
			d, ok := c.Instance.Data.(Row)
			if c.Instance.Noun != "project" || !ok {
				return ochazuke.Failed(errors.New("project open needs --target NAME, a name from project list"))
			}
			return ochazuke.Done(d)
		}, "print the target repository's manifest row").Fetch()
	r.Define("template").Columns(templateColumns...).
		Verb("list", func(ochazuke.Ctx) ochazuke.Result { return result(loadTemplates()) },
			"the template catalog: {id, label, data: {name, kind, path}, cells}").Rows("template").Fetch()
	// template is a facts noun over its list verb (Runtime.DefineListFacts);
	// project is a record noun over the manifest (store.go). project create is project new under the core name
	// (create ROOT/NAME and record it): the same handler, parameters and
	// history.
	r.DefineListFacts(ochazuke.ListFacts{Noun: "template", List: func(ochazuke.Ctx) ochazuke.Result { return result(loadTemplates()) }})
	create := r.Operations.At("project.new")
	create.Key, create.Standard, create.Unavailable = ochazuke.KeyOf("project", "create"), "", ""
	create.Description += " (as project.new)"
	if create.AppHandler != nil {
		create.Run = nil
	}
	r.Register(create)
	// The verbs that write: the manifest, the hub links, clones and pulls.
	setHistory(r, ochazuke.HistoryChange, "project.new", "project.create", "project.adopt", "project.link", "project.unlink", "project.sync", "project.clone", "project.seed")
	params := func(key string, p map[string]ochazuke.Parameter) {
		op := r.Operations.At(ochazuke.Key(r.CanonicalKey(key)))
		op.Parameters = p
		r.Operations.Set(op)
	}
	params("project.new", map[string]ochazuke.Parameter{
		"name":     {Type: "string", Required: true, Description: "the project directory's name under root"},
		"root":     {Type: "string", Description: "directory the project is created in (default ~/proj)"},
		"template": {Type: "string", Description: "a template name from project templates, or a path"},
		"github":   {Type: "boolean", Description: "create github.com/" + t.GitHubOwner() + "/NAME and push ($O_PROJ_GITHUB_OWNER, else " + DefaultGitHubOwner + ")"},
		"private":  {Type: "boolean", Description: "the GitHub repository is private (default true)"},
		"license":  {Type: "string", Enum: Licenses, Description: "the LICENSE written where absent (default mit)"},
		"existing": {Type: "boolean", Description: "adopt a directory that already has files"},
		"mtm":      {Type: "boolean", Description: "ask mtm for a work project (default true)"},
		"hub":      {Type: "string", Description: "the hub directory that links NAME -> DIR (default $O_PROJ_HUB, else ~/hub/o); none skips"},
		"family":   {Type: "boolean", Description: "link the project into the o/oden family hub (default true); false skips"},
	})
	params("project.create", r.Operations.At("project.new").Parameters)
	params("project.adopt", map[string]ochazuke.Parameter{
		"path": {Type: "string", Required: true, Description: "the repository's directory"},
		"name": {Type: "string", Description: "the manifest name (default: the directory's base name)"},
		"hub":  {Type: "string", Description: "the hub directory (default $O_PROJ_HUB, else ~/hub/o); none skips the link"},
	})
	params("project.link", map[string]ochazuke.Parameter{
		"name": {Type: "string", Description: "the repository to link (or all)"},
		"all":  {Type: "boolean", Description: "link every repository in the manifest"},
		"hub":  {Type: "string", Description: "the hub directory (default $O_PROJ_HUB, else ~/hub/o)"},
	})
	params("project.unlink", map[string]ochazuke.Parameter{
		"name": {Type: "string", Description: "the repository to unlink (or all)"},
		"all":  {Type: "boolean", Description: "unlink every repository in the manifest"},
		"hub":  {Type: "string", Description: "the hub directory (default $O_PROJ_HUB, else ~/hub/o)"},
	})
	params("project.sync", map[string]ochazuke.Parameter{
		"pull":    {Type: "boolean", Description: "git pull --ff-only in clean repositories that are behind"},
		"dry_run": {Type: "boolean", Description: "report only: no clone, link, fetch or pull"},
	})
	params("project.clone", map[string]ochazuke.Parameter{
		"name":    {Type: "string", Description: "the repository to clone (default: every one missing on this machine)", Noun: "project", Free: true},
		"more":    {Type: "string", Rest: true, Description: "further repository names after the first"},
		"hub":     {Type: "string", Description: "the hub directory to link into (default $O_PROJ_HUB, else ~/hub/o); none skips links"},
		"dry_run": {Type: "boolean", Description: "report only: no clone or link"},
	})
	params("project.seed", map[string]ochazuke.Parameter{
		"hub":       {Type: "string", Description: "the hub directory whose links are read (default $O_PROJ_HUB, else ~/hub/o); none skips"},
		"inventory": {Type: "string", Description: "o-inventory's repos.json (default under the config directory); none skips"},
		"mtm_json":  {Type: "string", Description: "a saved o-proj list from the version that read mtm's store; rows with origin o-proj are read", Free: true},
		"dry_run":   {Type: "boolean", Description: "report what would be added; write nothing"},
	})
	params("project.list", map[string]ochazuke.Parameter{
		"archived": {Type: "boolean", Description: "include archived repositories"},
	})
	params("project.check", map[string]ochazuke.Parameter{
		"dir": {Type: "string", Description: "the directory to check (default the working directory)"},
	})
	sh := ocli.New(r, "o-proj", os.Stdin, t.Stdout, t.Stderr)
	sh.Usage = ocli.DefaultUsage(t.Prog)
	sh.Reveal = func(string) error {
		if _, err := load(true); err != nil {
			return err
		}
		_, err := loadTemplates()
		return err
	}
	return sh, nil
}

// newFromArgs runs New with the flags project new's arguments spell; its
// result is the project's name and directory.
func (t *Tool) newFromArgs(args ochazuke.Arguments) (map[string]string, error) {
	name := ochazuke.StringArg(args, "name", "")
	root := ochazuke.StringArg(args, "root", t.DefaultRoot())
	argv := []string{"--root", root}
	for _, flag := range []string{"template", "license", "hub"} {
		if v := ochazuke.StringArg(args, flag, ""); v != "" {
			argv = append(argv, "--"+flag, v)
		}
	}
	for flag, when := range map[string]struct {
		name string
		want bool
	}{"--github": {"github", true}, "--existing": {"existing", true}, "--no-mtm": {"mtm", false}, "--private": {"private", true}, "--public": {"private", false}, "--family=false": {"family", false}} {
		if v, set := args[when.name].(bool); set && v == when.want {
			argv = append(argv, flag)
		}
	}
	if err := t.New(append(argv, "--", name)); err != nil {
		return nil, err
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return map[string]string{"name": name, "dir": filepath.Join(base, name)}, nil
}

// setHistory declares the history kind (Operation.HistoryKind) of each
// named verb, so none is left to ochazuke's default: NounBuilder.Fetch
// marks a read where it is declared, and the builder has no method for a
// change. An unknown key panics, so a renamed verb is renamed here too.
func setHistory(r *ochazuke.Runtime, kind string, keys ...string) {
	for _, key := range keys {
		op, ok := r.Operations.Find(ochazuke.Key(r.CanonicalKey(key)))
		if !ok {
			panic("setHistory: no operation " + key)
		}
		op.HistoryKind = kind
		r.Operations.Set(op)
	}
}

// namesArg is the repository names a verb was given: its name and the rest.
func namesArg(args ochazuke.Arguments) []string {
	var names []string
	for _, key := range []string{"name", "more"} {
		if v := ochazuke.StringArg(args, key, ""); v != "" {
			names = append(names, strings.Fields(v)...)
		}
	}
	return names
}
