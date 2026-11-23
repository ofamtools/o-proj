package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ofam.dev/ochazuke/config"
	"ofam.dev/ochazuke/core"
	otea "ofam.dev/ochazuke/tea"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/form"
	"ofam.dev/ochazuke/widgets/input"
	"ofam.dev/ochazuke/widgets/markdown"
	"ofam.dev/ochazuke/widgets/picker"
	"ofam.dev/ochazuke/widgets/table"
)

// Frame and node IDs for the Ochazuke v2 TUI.
const (
	pageID       = "main"
	nodeGrid     = "grid"
	nodeFilter   = "filter"
	nodeDetail   = "detail"
	modalPalette = "palette"
	modalHelp    = "help"
	modalConfirm = "confirm"
	modalForm    = "form"
)

var (
	sortKeys  = []string{"name", "status", "origin", "license", "linked", "branch"}
	groupKeys = []string{"", "origin", "status", "linked", "license"}
	densities = []string{ui.DensityBalanced, ui.DensityCompact, ui.DensitySpacious}
	tuiModes  = []core.Mode{
		{Name: "normal"},
		{Name: "input"},
		{Name: "palette"},
		{Name: "help"},
		{Name: "confirm"},
		{Name: "form"},
	}
	defaultSettings = map[string]any{
		"matchMode":   "substring",
		"detail":      true,
		"detailRatio": 0.58,
		"stackWidth":  100,
		"density":     "balanced",
	}
)

// ProjectItem combines a manifest Entry with its live git status and derived fields.
type ProjectItem struct {
	Entry   Entry
	Dir     string
	License string
	Linked  string
	Status  RepoStatus
}

// Page is the TUI state stored by value in the page frame's Data.
type Page struct {
	Items        []ProjectItem
	Summary      Summary
	Loading      bool
	Loaded       bool
	ShowArchived bool
	LoadErr      string

	base   []table.Row
	byName map[string]ProjectItem
	Grid   table.Model

	Query     string
	QueryLine input.Line
	Typing    bool
	Match     table.MatchMode
	FilterErr string

	Sort string
	Desc bool

	Detail    bool
	Ratio     float64
	DetailOff int
	detailRow string
	MD        *markdown.Model

	ThemeIdx int
	Density  string

	Status, Tone string
}

func (p Page) clone() Page {
	p.Grid = p.Grid.Copy()
	p.QueryLine.Text = slices.Clone(p.QueryLine.Text)
	if p.MD != nil {
		cp := *p.MD
		p.MD = &cp
	}
	return p
}

func pageOf(s core.State) Page {
	p, _ := s.Pages[pageID].Data.(Page)
	return p
}

func withPage(s core.State, p Page) core.State {
	f := s.Pages[pageID]
	f.Data = p
	s.Pages = maps.Clone(s.Pages)
	s.Pages[pageID] = f
	return s
}

func (p *Page) note(msg string) { p.Status, p.Tone = msg, "success" }
func (p *Page) info(msg string) { p.Status, p.Tone = msg, "info" }
func (p *Page) warn(msg string) { p.Status, p.Tone = msg, "warning" }
func (p *Page) fail(msg string) { p.Status, p.Tone = msg, "error" }

var gridColumns = []table.Column{
	{Key: "name", Title: "project"},
	{Key: "status", Title: "status"},
	{Key: "branch", Title: "branch"},
	{Key: "origin", Title: "origin"},
	{Key: "license", Title: "license"},
	{Key: "linked", Title: "linked"},
	{Key: "path", Title: "path"},
}

func (p *Page) rebuild() {
	p.base = make([]table.Row, 0, len(p.Items))
	p.byName = make(map[string]ProjectItem, len(p.Items))
	for _, it := range p.Items {
		p.byName[it.Entry.Name] = it
		if it.Entry.Archived && !p.ShowArchived {
			continue
		}
		stLabel := formatStatusBadge(it.Status)
		if it.Entry.Archived {
			stLabel += " [arch]"
		}
		branch := firstNonEmpty(it.Status.Branch, it.Entry.Branch)
		p.base = append(p.base, table.Row{
			ID:  it.Entry.Name,
			Ref: &core.Ref{Kind: KindProject, ID: it.Entry.Name},
			Cells: map[string]any{
				"name":    it.Entry.Name,
				"status":  stLabel,
				"branch":  branch,
				"origin":  it.Entry.Origin,
				"license": it.License,
				"linked":  it.Linked,
				"path":    it.Entry.Path,
			},
		})
	}
	p.Grid.Columns = gridColumns
	for id := range p.Grid.Marked {
		if _, ok := p.byName[id]; !ok {
			delete(p.Grid.Marked, id)
		}
	}
}

func (p *Page) reproject() {
	keep, onRow := p.focusedName()
	tmp := table.Model{Columns: gridColumns, Rows: p.base, Query: p.Query, MatchMode: p.Match}
	rows := tmp.Filtered()
	p.FilterErr = tmp.FilterErr
	if p.FilterErr != "" {
		rows = nil
	}
	rows = slices.Clone(rows)
	slices.SortStableFunc(rows, func(x, y table.Row) int {
		c := p.compare(x.ID, y.ID)
		if p.Desc {
			return -c
		}
		return c
	})
	p.Grid.Rows = rows
	if onRow {
		p.focusName(keep)
	}
	p.Grid.Focused()
}

func (p *Page) compare(x, y string) int {
	a, b := p.byName[x], p.byName[y]
	switch p.Sort {
	case "status":
		if a.Status.Status != b.Status.Status {
			return strings.Compare(a.Status.Status, b.Status.Status)
		}
		if a.Status.Changed != b.Status.Changed {
			if a.Status.Changed < b.Status.Changed {
				return -1
			}
			return 1
		}
	case "origin":
		if a.Entry.Origin != b.Entry.Origin {
			return strings.Compare(a.Entry.Origin, b.Entry.Origin)
		}
	case "license":
		if a.License != b.License {
			return strings.Compare(a.License, b.License)
		}
	case "linked":
		if a.Linked != b.Linked {
			return strings.Compare(a.Linked, b.Linked)
		}
	case "branch":
		ba, bb := firstNonEmpty(a.Status.Branch, a.Entry.Branch), firstNonEmpty(b.Status.Branch, b.Entry.Branch)
		if ba != bb {
			return strings.Compare(ba, bb)
		}
	}
	return strings.Compare(a.Entry.Name, b.Entry.Name)
}

func (p *Page) focusedName() (string, bool) {
	r, ok := p.Grid.FocusedRow()
	return r.ID, ok
}

func (p *Page) focusName(id string) bool {
	for i, it := range p.Grid.Items() {
		if !it.Header && it.Row.ID == id {
			p.Grid.SelectedIdx = i
			p.Grid.Focused()
			return true
		}
	}
	return false
}

func (p *Page) targets() []string {
	var out []string
	for id := range p.Grid.Marked {
		if _, ok := p.byName[id]; ok {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		if id, ok := p.focusedName(); ok {
			out = []string{id}
		}
	}
	return out
}

// UIConfig holds optional startup overrides for the TUI.
type UIConfig struct {
	Bindings     []core.Binding
	Theme        *ui.Theme
	Settings     *config.Settings
	ShowArchived bool
	Notes        []string
}

// UI is the immutable TUI configuration shared by handlers and the view.
type UI struct {
	Tool         *Tool
	Km           *core.Keymap
	Bindings     []core.Binding
	Specs        []core.CommandSpec
	Themes       []ui.Theme
	Match        table.MatchMode
	Ratio        float64
	StackW       int
	Detail       bool
	Density      string
	ShowArchived bool
	Notes        []string
}

func newUI(tl *Tool, cfg UIConfig) *UI {
	u := &UI{Tool: tl, ShowArchived: cfg.ShowArchived, Notes: cfg.Notes}
	base := ui.DefaultDarkTheme()
	if cfg.Theme != nil {
		base = *cfg.Theme
	}
	u.Themes = []ui.Theme{base, ui.DefaultLightTheme(), ui.HighContrastDarkTheme()}
	set := cfg.Settings
	if set == nil {
		set, _ = config.LoadSettings(nil, defaultSettings)
	}
	switch m := table.MatchMode(set.String("matchMode", "substring")); m {
	case table.MatchSubstring, table.MatchFuzzy, table.MatchRegex:
		u.Match = m
	default:
		u.Match = table.MatchSubstring
	}
	u.Ratio = min(max(set.Float("detailRatio", 0.58), 0.2), 0.8)
	u.StackW = set.Int("stackWidth", 100)
	u.Detail = set.Bool("detail", true)
	u.Density = set.String("density", ui.DensityBalanced)
	if !slices.Contains(densities, u.Density) {
		u.Density = ui.DensityBalanced
	}
	u.Specs = commandSpecs()
	u.Bindings = config.MergeBindings(defaultBindings(), cfg.Bindings)
	km, err := core.NewKeymap(tuiModes, u.Bindings, u.Specs...)
	if err != nil {
		u.Notes = append(u.Notes, "bindings: "+strings.ReplaceAll(err.Error(), "\n", "; "))
		if km == nil {
			km, _ = core.NewKeymap(tuiModes, defaultBindings(), u.Specs...)
		}
	}
	u.Km = km
	return u
}

func (u *UI) theme(p *Page) ui.Theme {
	t := u.Themes[p.ThemeIdx%len(u.Themes)]
	t.Density = p.Density
	return t
}

func commandSpecs() []core.CommandSpec {
	user := func(id, desc string) core.CommandSpec {
		return core.CommandSpec{ID: id, Description: desc, Tags: []string{"user"}}
	}
	internal := func(id, desc string) core.CommandSpec {
		return core.CommandSpec{ID: id, Description: desc}
	}
	specs := []core.CommandSpec{
		user("filter.open", "Filter projects across all columns"),
		user("filter.clear", "Clear active filter"),
		user("filter.mode.cycle", "Cycle filter match mode: substring, fuzzy, regex"),
		user("sort.cycle", "Cycle sort column: name, status, origin, license, linked, branch"),
		user("sort.reverse", "Reverse sort order"),
		user("group.cycle", "Cycle grouping: none, origin, status, linked, license"),
		user("group.collapse", "Collapse or expand focused group"),
		user("mark.toggle", "Mark or unmark focused project"),
		user("mark.clear", "Clear all marks"),
		user("detail.toggle", "Show or hide Markdown detail/report pane"),
		user("detail.scroll.down", "Scroll detail pane down"),
		user("detail.scroll.up", "Scroll detail pane up"),
		user("split.grow", "Widen project table"),
		user("split.shrink", "Narrow project table"),
		user("split.reset", "Reset detail split ratio"),
		user("project.refresh", "Reload manifest and git status (async)"),
		user("project.archived.toggle", "Show or hide archived projects"),
		user("project.new", "Create a new project repository (form)"),
		user("project.adopt", "Adopt an existing git repository (form)"),
		user("project.edit", "Edit focused project manifest entry (form)"),
		user("project.link", "Link focused/marked project(s) into hub"),
		user("project.unlink", "Unlink focused/marked project(s) from hub"),
		user("project.sync", "Fetch and report status for focused/marked project(s)"),
		user("project.archive.toggle", "Archive or restore focused project in manifest"),
		user("project.delete", "Remove focused project from manifest (asks first; keeps files)"),
		user("theme.cycle", "Cycle theme: dark, light, high contrast"),
		user("density.cycle", "Cycle density: balanced, compact, spacious"),
		user("palette.open", "Open command palette"),
		user("help.open", "Show active key bindings"),
		user("app.close", "Quit"),

		internal("project.delete.apply", "Remove project from manifest after confirm"),
		internal("scan.done", "Project reload completed"),
		internal("action.done", "Async project action completed"),
		internal("input.insert", "Insert text into filter"),
		internal("input.backspace", "Delete character before cursor in filter"),
		internal("input.accept", "Keep filter and return to normal mode"),
		internal("input.cancel", "Clear filter and return to normal mode"),
		internal("palette.insert", "Insert text into palette query"),
		internal("palette.backspace", "Delete character before cursor in palette"),
		internal("palette.close", "Close command palette"),
		internal(picker.CmdConfirm, "Run selected palette command"),
		internal(picker.CmdModeCycle, "Cycle palette match mode"),
		internal("help.close", "Close help overlay"),
		internal("confirm.yes", "Confirm action"),
		internal("confirm.no", "Cancel confirmation"),
		internal(form.CmdInsert, "Insert text into active form field"),
		internal(form.CmdBackspace, "Delete character before cursor in form"),
		internal(form.CmdToggle, "Toggle bool or cycle enum field"),
		internal(form.CmdSubmit, "Submit form"),
		internal(form.CmdCancel, "Cancel form"),
	}
	specs = append(specs, input.ReadlineSpecs()...)
	specs = append(specs, core.MotionSpecs()...)
	return specs
}

func kb(mode, keys, cmd, hint string) core.Binding {
	return core.Binding{Mode: mode, Keys: strings.Fields(keys), Command: cmd, Hint: hint}
}

func kbClass(mode, cls, cmd string, args map[string]any, hint string) core.Binding {
	return core.Binding{Mode: mode, Class: cls, Command: cmd, Args: args, Hint: hint}
}

var unusedMotions = []string{
	"motion.left", "motion.right", "motion.start", "motion.end",
	"motion.word.next", "motion.word.prev", "motion.word.end",
	"motion.group.next", "motion.group.prev",
}

func standardMotions(mode string) []core.Binding {
	var out []core.Binding
	for _, bd := range core.StandardBindings(mode) {
		if !slices.Contains(unusedMotions, bd.Command) {
			out = append(out, bd)
		}
	}
	return out
}

func defaultBindings() []core.Binding {
	text := map[string]any{"text": "$text"}
	out := []core.Binding{
		kb("normal", "/", "filter.open", "filter"),
		kb("normal", "esc", "filter.clear", "clear filter"),
		kb("normal", "alt+m", "filter.mode.cycle", "match mode"),
		kb("normal", "o", "sort.cycle", "sort"),
		kb("normal", "O", "sort.reverse", "reverse sort"),
		kb("normal", "g b", "group.cycle", "group"),
		kb("normal", "z a", "group.collapse", "fold group"),
		kb("normal", "space", "mark.toggle", "mark"),
		kb("normal", "M", "mark.clear", "unmark all"),
		kb("normal", "enter", "detail.toggle", "detail"),
		kb("normal", "J", "detail.scroll.down", "scroll detail down"),
		kb("normal", "K", "detail.scroll.up", "scroll detail up"),
		kb("normal", ">", "split.grow", "wider table"),
		kb("normal", "<", "split.shrink", "narrower table"),
		kb("normal", "=", "split.reset", "reset split"),
		kb("normal", "r", "project.refresh", "refresh"),
		kb("normal", "R", "project.refresh", "refresh"),
		kb("normal", "a", "project.archived.toggle", "archived"),
		kb("normal", "n", "project.new", "new"),
		kb("normal", "A", "project.adopt", "adopt"),
		kb("normal", "e", "project.edit", "edit"),
		kb("normal", "l", "project.link", "link"),
		kb("normal", "u", "project.unlink", "unlink"),
		kb("normal", "s", "project.sync", "sync"),
		kb("normal", "x", "project.archive.toggle", "archive"),
		kb("normal", "d d", "project.delete", "remove"),
		kb("normal", "g t", "theme.cycle", "theme"),
		kb("normal", "g d", "density.cycle", "density"),
		kb("normal", ":", "palette.open", "commands"),
		kb("normal", "?", "help.open", "help"),
		kb("normal", "q", "app.close", "quit"),
	}
	out = append(out, standardMotions("normal")...)
	out = append(out,
		kbClass("input", core.ClassPrintable, "input.insert", text, "type"),
		kbClass("input", core.ClassPaste, "input.insert", text, "paste"),
		kb("input", "backspace", "input.backspace", "delete"),
		kb("input", "enter", "input.accept", "keep filter"),
		kb("input", "esc", "input.cancel", "clear filter"),
		kb("input", "alt+m", "filter.mode.cycle", "match mode"),
		kb("input", "tab", "filter.mode.cycle", "match mode"),
		kb("input", "down", "motion.down", "next row"),
		kb("input", "ctrl+n", "motion.down", "next row"),
		kb("input", "up", "motion.up", "previous row"),
		kb("input", "ctrl+p", "motion.up", "previous row"),

		kbClass("palette", core.ClassPrintable, "palette.insert", text, "type"),
		kbClass("palette", core.ClassPaste, "palette.insert", text, "paste"),
		kb("palette", "backspace", "palette.backspace", "delete"),
		kb("palette", "enter", picker.CmdConfirm, "run"),
		kb("palette", "esc", "palette.close", "close"),
		kb("palette", "alt+m", picker.CmdModeCycle, "match mode"),
		kb("palette", "tab", picker.CmdModeCycle, "match mode"),
		kb("palette", "down", "motion.down", "next"),
		kb("palette", "ctrl+n", "motion.down", "next"),
		kb("palette", "up", "motion.up", "previous"),
		kb("palette", "ctrl+p", "motion.up", "previous"),

		kbClass("form", core.ClassPrintable, form.CmdInsert, text, "type"),
		kbClass("form", core.ClassPaste, form.CmdInsert, text, "paste"),
		kb("form", "backspace", form.CmdBackspace, "delete"),
		kb("form", "tab", "motion.down", "next field"),
		kb("form", "down", "motion.down", "next field"),
		kb("form", "shift+tab", "motion.up", "previous field"),
		kb("form", "up", "motion.up", "previous field"),
		kb("form", "enter", form.CmdSubmit, "submit"),
		kb("form", "esc", form.CmdCancel, "cancel"),

		kb("confirm", "y", "confirm.yes", "confirm"),
		kb("confirm", "Y", "confirm.yes", "confirm"),
		kb("confirm", "n", "confirm.no", "cancel"),
		kb("confirm", "N", "confirm.no", "cancel"),
		kb("confirm", "esc", "confirm.no", "cancel"),
		kb("confirm", "q", "confirm.no", "cancel"),

		kb("help", "esc", "help.close", "close"),
		kb("help", "q", "help.close", "close"),
		kb("help", "?", "help.close", "close"),
	)
	// Attach full Readline bindings to input, palette, and form modes without collisions.
	for _, mode := range []string{"input", "palette", "form"} {
		taken := map[string]bool{}
		for _, bd := range out {
			if bd.Mode == mode && len(bd.Keys) > 0 {
				taken[strings.Join(bd.Keys, " ")] = true
			}
		}
		for _, rb := range input.ReadlineBindings(mode, "") {
			if k := strings.Join(rb.Keys, " "); !taken[k] {
				taken[k] = true
				out = append(out, rb)
			}
		}
	}
	for _, mode := range []string{"help", "confirm"} {
		out = append(out, standardMotions(mode)...)
	}
	return out
}

// --- engine & handlers ---------------------------------------------------------------

type pageFn func(s core.State, p *Page, c core.Command) (core.State, []core.Effect)

type confirmData struct {
	Title, Text string
	Off         int
	Action      core.Command
}

type formData struct {
	Kind string // "new", "edit", "adopt"
	Name string // target project name for "edit"
	M    *form.Model
}

type scanResult struct {
	Items   []ProjectItem
	Summary Summary
}

type actionResult struct {
	Msg    string
	Reload bool
}

func (u *UI) wrap(f pageFn) core.Handler {
	return func(s core.State, c core.Command) core.StepResult {
		p := pageOf(s).clone()
		if c.Actor.Kind != "system" {
			p.Status, p.Tone = "", ""
		}
		p.Grid.PageSize = u.geometry(&p, s.Width, s.Height).rows
		s, effects := f(s, &p, c)
		if !p.Detail {
			p.DetailOff = 0
		} else if id, _ := p.focusedName(); id != p.detailRow {
			p.detailRow, p.DetailOff = id, 0
			u.syncMarkdown(&p)
		}
		return core.Next(withPage(s, p), effects...)
	}
}

func (u *UI) syncMarkdown(p *Page) {
	if p.MD == nil {
		p.MD = markdown.New("Detail", "")
	}
	it, ok := p.Grid.Focused()
	switch {
	case !ok && p.Loading:
		p.MD.Title = "Detail"
		p.MD.SetSource("_Loading manifest and git status..._")
	case !ok:
		p.MD.Title = "Detail"
		p.MD.SetSource("_No projects match the current filter._\n\nPress `n` to create a project or `A` to adopt an existing repository.")
	case it.Header:
		p.MD.Title = "Group"
		p.MD.SetSource(fmt.Sprintf("## %s: %s\n\n**%d** project(s) in this group.\n\nPress `z a` to fold or unfold.", p.Grid.GroupField, it.Group, it.Count))
	default:
		item := p.byName[it.Row.ID]
		p.MD.Title = item.Entry.Name
		p.MD.SetSource(u.Tool.projectMarkdown(item.Entry, &item.Status))
	}
	p.MD.Offset = p.DetailOff
}

func setPage(s core.State, mode, focus string) core.State {
	f := s.Pages[pageID]
	f.Mode, f.FocusID = mode, focus
	s.Pages = maps.Clone(s.Pages)
	s.Pages[pageID] = f
	return s
}

func openModal(s core.State, id, mode string, data any) core.State {
	s = closeModal(s, id)
	s.Modals = append(slices.Clone(s.Modals), core.Frame{ID: id, Kind: core.FrameModal, Mode: mode, FocusID: id, Data: data})
	return s
}

func closeModal(s core.State, id string) core.State {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		s.Modals = slices.Delete(slices.Clone(s.Modals), i, i+1)
	}
	return s
}

func modalData(s core.State, id string) any {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		return s.Modals[i].Data
	}
	return nil
}

func setModalData(s core.State, id string, data any) core.State {
	if i := slices.IndexFunc(s.Modals, func(f core.Frame) bool { return f.ID == id }); i >= 0 {
		s.Modals = slices.Clone(s.Modals)
		s.Modals[i].Data = data
	}
	return s
}

func dispatch(c core.Command) core.Effect { return core.CustomEffect{Kind: "dispatch", Data: c} }

func newEngine(u *UI) *core.Engine {
	e := core.NewEngine(u.Km)
	h := func(id string, f pageFn) { e.Handle(id, u.wrap(f)) }
	scoped := func(scope, id string, f pageFn) { e.HandleScope(scope, id, u.wrap(f)) }

	for _, id := range []string{
		"motion.down", "motion.up", "motion.top", "motion.bottom",
		"motion.page.down", "motion.page.up", "motion.half.down", "motion.half.up",
		"motion.block.next", "motion.block.prev",
	} {
		scoped("grid", id, func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
			if (c.ID == "motion.bottom" || c.ID == "motion.top") && c.Count > 0 {
				p.Grid.SelectedIdx = c.Count - 1
				p.Grid.Focused()
				return s, nil
			}
			p.Grid.HandleCommand(c)
			return s, nil
		})
		scoped("palette", id, u.paletteCmd)
		scoped("help", id, u.scrollModal(modalHelp))
		scoped("confirm", id, u.scrollModal(modalConfirm))
		scoped("form", id, u.formCmd)
	}

	// Filter & Readline
	h("filter.open", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Typing = true
		p.QueryLine.Sync(p.Query)
		return setPage(s, "input", nodeFilter), nil
	})
	h("filter.clear", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Query = ""
		p.QueryLine.Set("")
		p.reproject()
		u.syncMarkdown(p)
		return s, nil
	})
	h("filter.mode.cycle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		m := table.Model{MatchMode: p.Match}
		m.CycleMatchMode()
		p.Match = m.MatchMode
		p.reproject()
		u.syncMarkdown(p)
		p.info("match mode: " + string(p.Match))
		return s, nil
	})
	h("input.insert", func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
		p.QueryLine.Sync(p.Query)
		p.QueryLine.Insert(c.Arg("text"))
		p.Query = p.QueryLine.Value()
		p.reproject()
		u.syncMarkdown(p)
		return s, nil
	})
	h("input.backspace", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.QueryLine.Sync(p.Query)
		p.QueryLine.HandleCommand(core.Command{ID: input.CmdBackspace})
		p.Query = p.QueryLine.Value()
		p.reproject()
		u.syncMarkdown(p)
		return s, nil
	})
	h("input.accept", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Typing = false
		return setPage(s, "normal", nodeGrid), nil
	})
	h("input.cancel", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Typing = false
		p.Query = ""
		p.QueryLine.Set("")
		p.reproject()
		u.syncMarkdown(p)
		return setPage(s, "normal", nodeGrid), nil
	})

	for _, id := range input.ReadlineCommandIDs() {
		scoped("grid", id, func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
			if !p.Typing {
				return s, nil
			}
			p.QueryLine.Sync(p.Query)
			if changed, _ := p.QueryLine.HandleCommand(c); changed {
				p.Query = p.QueryLine.Value()
				p.reproject()
				u.syncMarkdown(p)
			}
			return s, nil
		})
		scoped("palette", id, u.paletteCmd)
		scoped("form", id, u.formCmd)
	}

	// Sort, group, marks
	h("sort.cycle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		i := slices.Index(sortKeys, p.Sort)
		p.Sort = sortKeys[(i+1)%len(sortKeys)]
		p.reproject()
		u.syncMarkdown(p)
		p.info("sort: " + p.Sort)
		return s, nil
	})
	h("sort.reverse", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Desc = !p.Desc
		p.reproject()
		u.syncMarkdown(p)
		return s, nil
	})
	h("group.cycle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		i := slices.Index(groupKeys, p.Grid.GroupField)
		p.Grid.GroupField = groupKeys[(i+1)%len(groupKeys)]
		p.Grid.Collapsed = map[string]bool{}
		p.Grid.Focused()
		u.syncMarkdown(p)
		if p.Grid.GroupField == "" {
			p.info("ungrouped")
		} else {
			p.info("grouped by " + p.Grid.GroupField)
		}
		return s, nil
	})
	h("group.collapse", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Grid.ToggleGroup()
		u.syncMarkdown(p)
		return s, nil
	})
	h("mark.toggle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Grid.ToggleMark()
		p.Grid.HandleCommand(core.Command{ID: "motion.down"})
		return s, nil
	})
	h("mark.clear", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Grid.Marked = map[string]bool{}
		p.info("marks cleared")
		return s, nil
	})

	// Detail & split
	h("detail.toggle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Detail = !p.Detail
		if p.Detail {
			u.syncMarkdown(p)
		}
		return s, nil
	})
	h("detail.scroll.down", func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
		step := max(c.Count, 1) * 3
		p.DetailOff += step
		if p.MD != nil {
			p.MD.Offset = p.DetailOff
		}
		return s, nil
	})
	h("detail.scroll.up", func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
		step := max(c.Count, 1) * 3
		p.DetailOff = max(0, p.DetailOff-step)
		if p.MD != nil {
			p.MD.Offset = p.DetailOff
		}
		return s, nil
	})
	h("split.grow", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Ratio = min(0.8, p.Ratio+0.05)
		return s, nil
	})
	h("split.shrink", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Ratio = max(0.2, p.Ratio-0.05)
		return s, nil
	})
	h("split.reset", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Ratio = u.Ratio
		return s, nil
	})

	// Theme & density
	h("theme.cycle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.ThemeIdx = (p.ThemeIdx + 1) % len(u.Themes)
		th := u.theme(p)
		p.info("theme: " + th.Name)
		return s, []core.Effect{core.CustomEffect{Kind: "theme", Data: th}}
	})
	h("density.cycle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		i := slices.Index(densities, p.Density)
		p.Density = densities[(i+1)%len(densities)]
		th := u.theme(p)
		p.info("density: " + p.Density)
		return s, []core.Effect{core.CustomEffect{Kind: "theme", Data: th}}
	})

	// Project loading & actions
	h("project.refresh", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.Loading = true
		p.LoadErr = ""
		return s, []core.Effect{u.loadEffect()}
	})
	h("scan.done", func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
		p.Loading = false
		p.Loaded = true
		if err, ok := c.Args["error"].(error); ok && err != nil {
			p.LoadErr = err.Error()
			p.fail("load failed: " + err.Error())
			return s, nil
		}
		if res, ok := c.Args["value"].(scanResult); ok {
			p.Items = res.Items
			p.Summary = res.Summary
			p.rebuild()
			p.reproject()
			u.syncMarkdown(p)
		}
		return s, nil
	})
	h("action.done", func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
		if err, ok := c.Args["error"].(error); ok && err != nil {
			p.fail(err.Error())
			return s, nil
		}
		if res, ok := c.Args["value"].(actionResult); ok {
			if res.Msg != "" {
				p.note(res.Msg)
			}
			if res.Reload {
				p.Loading = true
				return s, []core.Effect{u.loadEffect()}
			}
		}
		return s, nil
	})
	h("project.archived.toggle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.ShowArchived = !p.ShowArchived
		p.rebuild()
		p.reproject()
		u.syncMarkdown(p)
		if p.ShowArchived {
			p.info("showing archived projects")
		} else {
			p.info("hiding archived projects")
		}
		return s, nil
	})
	h("project.link", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		names := p.targets()
		if len(names) == 0 {
			p.warn("no project selected")
			return s, nil
		}
		return s, []core.Effect{u.asyncAction(func() (actionResult, error) {
			linkedN := 0
			for _, name := range names {
				cnt, err := u.quietTool().LinkProjects(u.Tool.DefaultHub(), name, false)
				if err != nil {
					return actionResult{}, err
				}
				linkedN += cnt.Linked
			}
			return actionResult{Msg: fmt.Sprintf("linked %d project(s)", linkedN), Reload: true}, nil
		})}
	})
	h("project.unlink", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		names := p.targets()
		if len(names) == 0 {
			p.warn("no project selected")
			return s, nil
		}
		return s, []core.Effect{u.asyncAction(func() (actionResult, error) {
			unlinkedN := 0
			for _, name := range names {
				cnt, err := u.quietTool().UnlinkProjects(u.Tool.DefaultHub(), name, false)
				if err != nil {
					return actionResult{}, err
				}
				unlinkedN += cnt.Unlinked
			}
			return actionResult{Msg: fmt.Sprintf("unlinked %d project(s)", unlinkedN), Reload: true}, nil
		})}
	})
	h("project.sync", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		names := p.targets()
		if len(names) == 0 {
			p.warn("no project selected")
			return s, nil
		}
		p.info(fmt.Sprintf("syncing %d project(s)...", len(names)))
		return s, []core.Effect{u.asyncAction(func() (actionResult, error) {
			results, err := u.quietTool().SyncEntries(context.Background(), names, SyncOptions{
				Hub:   u.Tool.DefaultHub(),
				Fetch: true,
			})
			if err != nil {
				return actionResult{}, err
			}
			return actionResult{Msg: fmt.Sprintf("synced %d project(s)", len(results)), Reload: true}, nil
		})}
	})
	h("project.archive.toggle", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		name, ok := p.focusedName()
		if !ok {
			p.warn("no project selected")
			return s, nil
		}
		item := p.byName[name]
		next := !item.Entry.Archived
		return s, []core.Effect{u.asyncAction(func() (actionResult, error) {
			if _, err := u.Tool.updateEntry(name, func(e *Entry) { e.Archived = next }); err != nil {
				return actionResult{}, err
			}
			verb := "archived"
			if !next {
				verb = "restored"
			}
			return actionResult{Msg: fmt.Sprintf("%s %s", verb, name), Reload: true}, nil
		})}
	})
	h("project.delete", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		name, ok := p.focusedName()
		if !ok {
			p.warn("no project selected")
			return s, nil
		}
		item := p.byName[name]
		msg := fmt.Sprintf("Remove %q (%s) from %s?\n\nThe repository directory, hub link, and GitHub repo stay untouched.",
			name, item.Entry.Path, u.Tool.tilde(u.Tool.ManifestPath()))
		return openModal(s, modalConfirm, "confirm", confirmData{
			Title:  "Remove from manifest",
			Text:   msg,
			Action: core.Command{ID: "project.delete.apply", Args: map[string]any{"name": name}},
		}), nil
	})
	h("project.delete.apply", func(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
		name := c.Arg("name")
		return s, []core.Effect{u.asyncAction(func() (actionResult, error) {
			if err := u.Tool.deleteEntry(name); err != nil {
				return actionResult{}, err
			}
			return actionResult{Msg: fmt.Sprintf("removed %s from manifest", name), Reload: true}, nil
		})}
	})

	// Forms: new, edit, adopt
	h("project.new", func(s core.State, _ *Page, _ core.Command) (core.State, []core.Effect) {
		specs := []core.ParamSpec{
			{Name: "name", Type: core.ParamString, Required: true, Description: "Project name (directory under root)"},
			{Name: "root", Type: core.ParamPath, Default: u.Tool.tilde(u.Tool.DefaultRoot()), Description: "Parent directory"},
			{Name: "template", Type: core.ParamString, Description: "Optional template name or path"},
			{Name: "license", Type: core.ParamString, Enum: Licenses, Default: Licenses[0], Description: "License file"},
			{Name: "hub", Type: core.ParamPath, Default: u.Tool.tilde(u.Tool.expand(u.Tool.DefaultHub())), Description: "Hub directory (or none)"},
			{Name: "no_mtm", Type: core.ParamBool, Default: false, Description: "Skip creating mtm work project"},
		}
		m := form.New("New Project", specs, nil)
		m.Editing = true
		m.HandleCommand(core.Command{ID: form.CmdEdit})
		return openModal(s, modalForm, "form", formData{Kind: "new", M: m}), nil
	})
	h("project.adopt", func(s core.State, _ *Page, _ core.Command) (core.State, []core.Effect) {
		specs := []core.ParamSpec{
			{Name: "path", Type: core.ParamPath, Required: true, Description: "Existing git repository path"},
			{Name: "name", Type: core.ParamString, Description: "Manifest name (default: directory basename)"},
			{Name: "hub", Type: core.ParamPath, Default: u.Tool.tilde(u.Tool.expand(u.Tool.DefaultHub())), Description: "Hub directory (or none)"},
		}
		m := form.New("Adopt Repository", specs, nil)
		m.Editing = true
		m.HandleCommand(core.Command{ID: form.CmdEdit})
		return openModal(s, modalForm, "form", formData{Kind: "adopt", M: m}), nil
	})
	h("project.edit", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		name, ok := p.focusedName()
		if !ok {
			p.warn("no project selected")
			return s, nil
		}
		e := p.byName[name].Entry
		specs := []core.ParamSpec{
			{Name: "path", Type: core.ParamPath, Required: true, Description: "Repository path (~ for home)"},
			{Name: "url", Type: core.ParamString, Description: "Origin remote URL"},
			{Name: "branch", Type: core.ParamString, Description: "Default branch"},
			{Name: "origin", Type: core.ParamString, Enum: []string{OriginNew, OriginAdopted, ""}, Description: "Origin"},
			{Name: "archived", Type: core.ParamBool, Description: "Archived (hidden from default list)"},
		}
		init := map[string]any{
			"path":     e.Path,
			"url":      e.URL,
			"branch":   e.Branch,
			"origin":   e.Origin,
			"archived": e.Archived,
		}
		m := form.New("Edit "+name, specs, init)
		m.Editing = true
		m.HandleCommand(core.Command{ID: form.CmdEdit})
		return openModal(s, modalForm, "form", formData{Kind: "edit", Name: name, M: m}), nil
	})
	for _, id := range []string{form.CmdInsert, form.CmdBackspace, form.CmdToggle, form.CmdSubmit, form.CmdCancel} {
		scoped("form", id, u.formCmd)
	}

	// Palette, help, confirm, close
	h("palette.open", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		var items []picker.Item
		for _, sp := range u.Specs {
			if !slices.Contains(sp.Tags, "user") {
				continue
			}
			items = append(items, picker.Item{ID: sp.ID, Title: sp.ID, Subtitle: sp.Description})
		}
		m := picker.New("Commands", items)
		m.MatchMode = picker.MatchMode(p.Match)
		return openModal(s, modalPalette, "palette", m), nil
	})
	for _, id := range []string{"palette.insert", "palette.backspace", "palette.close", picker.CmdConfirm, picker.CmdModeCycle} {
		scoped("palette", id, u.paletteCmd)
	}
	h("help.open", func(s core.State, _ *Page, _ core.Command) (core.State, []core.Effect) {
		return openModal(s, modalHelp, "help", 0), nil
	})
	h("help.close", func(s core.State, _ *Page, _ core.Command) (core.State, []core.Effect) {
		return closeModal(s, modalHelp), nil
	})
	h("confirm.yes", func(s core.State, _ *Page, _ core.Command) (core.State, []core.Effect) {
		d, ok := modalData(s, modalConfirm).(confirmData)
		s = closeModal(s, modalConfirm)
		if !ok {
			return s, nil
		}
		return s, []core.Effect{dispatch(d.Action)}
	})
	h("confirm.no", func(s core.State, p *Page, _ core.Command) (core.State, []core.Effect) {
		p.info("cancelled")
		return closeModal(s, modalConfirm), nil
	})
	h("app.close", func(s core.State, _ *Page, _ core.Command) (core.State, []core.Effect) {
		return s, []core.Effect{core.QuitEffect{}}
	})

	return e
}

func (u *UI) quietTool() *Tool {
	cp := *u.Tool
	cp.Stdout = discard{}
	cp.Stderr = discard{}
	return &cp
}

func (u *UI) loadEffect() core.Effect {
	return core.AsyncEffect{
		Slot:    "scan",
		Command: "scan.done",
		Run: func(ctx context.Context) (any, error) {
			entries, err := u.Tool.Manifest()
			if err != nil {
				return nil, err
			}
			repos := make([]Repo, 0, len(entries))
			for _, e := range entries {
				repos = append(repos, Repo{Name: e.Name, Path: u.Tool.Dir(e)})
			}
			rep := collect(ctx, repos)
			byPath := make(map[string]RepoStatus, len(rep.Repos))
			for _, st := range rep.Repos {
				byPath[st.Path] = st
			}
			items := make([]ProjectItem, 0, len(entries))
			for _, e := range entries {
				dir := u.Tool.Dir(e)
				st := byPath[dir]
				items = append(items, ProjectItem{
					Entry:   e,
					Dir:     dir,
					License: licenseOf(dir),
					Linked:  u.Tool.linkedIn(u.Tool.DefaultHub(), e.Name, dir),
					Status:  st,
				})
			}
			return scanResult{Items: items, Summary: rep.Summary}, nil
		},
	}
}

func (u *UI) asyncAction(fn func() (actionResult, error)) core.Effect {
	return core.AsyncEffect{
		Command: "action.done",
		Run: func(_ context.Context) (any, error) {
			return fn()
		},
	}
}

func (u *UI) scrollModal(id string) pageFn {
	return func(s core.State, _ *Page, c core.Command) (core.State, []core.Effect) {
		step := max(c.Count, 1)
		delta := 0
		switch c.ID {
		case "motion.down":
			delta = step
		case "motion.up":
			delta = -step
		case "motion.page.down", "motion.half.down":
			delta = 10 * step
		case "motion.page.up", "motion.half.up":
			delta = -10 * step
		case "motion.top":
			delta = -1 << 20
		case "motion.bottom":
			delta = 1 << 20
		}
		switch d := modalData(s, id).(type) {
		case int:
			return setModalData(s, id, max(0, d+delta)), nil
		case confirmData:
			d.Off = max(0, d.Off+delta)
			return setModalData(s, id, d), nil
		}
		return s, nil
	}
}

func (u *UI) paletteCmd(s core.State, _ *Page, c core.Command) (core.State, []core.Effect) {
	if c.ID == "palette.close" {
		return closeModal(s, modalPalette), nil
	}
	orig, ok := modalData(s, modalPalette).(*picker.Model)
	if !ok || orig == nil {
		return s, nil
	}
	m := *orig
	switch c.ID {
	case "palette.insert":
		c = core.Command{ID: input.CmdInsert, Args: c.Args}
	case "palette.backspace":
		c = core.Command{ID: input.CmdBackspace}
	}
	picked, _ := m.HandleCommand(c)
	if picked != nil {
		s = closeModal(s, modalPalette)
		return s, []core.Effect{dispatch(core.Command{ID: picked.ID, Source: "palette", Actor: core.Actor{Kind: "user"}})}
	}
	return setModalData(s, modalPalette, &m), nil
}

func (u *UI) formCmd(s core.State, p *Page, c core.Command) (core.State, []core.Effect) {
	fd, ok := modalData(s, modalForm).(formData)
	if !ok || fd.M == nil {
		return s, nil
	}
	m := *fd.M
	m.Values = maps.Clone(fd.M.Values)
	m.Errors = maps.Clone(fd.M.Errors)
	fd.M = &m
	if c.ID == form.CmdInsert && c.Arg("text") == " " && (m.KindAt(m.ActiveIdx) == form.KindBool || m.KindAt(m.ActiveIdx) == form.KindEnum) {
		c = core.Command{ID: form.CmdToggle}
	}
	res, _ := m.HandleCommand(c)
	if c.ID == "motion.down" || c.ID == "motion.up" {
		m.Editing = m.Editable()
		m.HandleCommand(core.Command{ID: form.CmdEdit})
	}
	switch {
	case res.Cancelled:
		p.info("form cancelled")
		return closeModal(s, modalForm), nil
	case res.Submitted:
		s = closeModal(s, modalForm)
		return s, []core.Effect{u.submitForm(fd, res.Values)}
	case len(res.ErrorFields) > 0:
		var msgs []string
		for _, name := range res.ErrorFields {
			msgs = append(msgs, name+": "+m.Errors[name])
		}
		p.fail("form error: " + strings.Join(msgs, "; "))
	}
	return setModalData(s, modalForm, fd), nil
}

func (u *UI) submitForm(fd formData, vals map[string]any) core.Effect {
	str := func(k string) string {
		if v, ok := vals[k]; ok && v != nil {
			return strings.TrimSpace(fmt.Sprint(v))
		}
		return ""
	}
	switch fd.Kind {
	case "new":
		return u.asyncAction(func() (actionResult, error) {
			name := str("name")
			argv := []string{name}
			if root := str("root"); root != "" {
				argv = append(argv, "--root", u.Tool.expand(root))
			}
			if tpl := str("template"); tpl != "" {
				argv = append(argv, "--template", tpl)
			}
			if lic := str("license"); lic != "" {
				argv = append(argv, "--license", lic)
			}
			if hub := str("hub"); hub != "" {
				argv = append(argv, "--hub", hub)
			}
			if noMtm, _ := vals["no_mtm"].(bool); noMtm {
				argv = append(argv, "--no-mtm")
			}
			if err := u.quietTool().New(argv); err != nil {
				return actionResult{}, err
			}
			return actionResult{Msg: "created project " + name, Reload: true}, nil
		})
	case "adopt":
		return u.asyncAction(func() (actionResult, error) {
			res, err := u.quietTool().AdoptPath(str("path"), str("name"), firstNonEmpty(str("hub"), u.Tool.DefaultHub()))
			if err != nil {
				return actionResult{}, err
			}
			return actionResult{Msg: fmt.Sprintf("adopted %s (%s)", res.Entry.Name, res.Outcome), Reload: true}, nil
		})
	case "edit":
		return u.asyncAction(func() (actionResult, error) {
			_, err := u.Tool.updateEntry(fd.Name, func(e *Entry) {
				if p := str("path"); p != "" {
					e.Path = p
				}
				e.URL = str("url")
				e.Branch = str("branch")
				e.Origin = str("origin")
				if arch, ok := vals["archived"].(bool); ok {
					e.Archived = arch
				}
			})
			if err != nil {
				return actionResult{}, err
			}
			return actionResult{Msg: "updated " + fd.Name, Reload: true}, nil
		})
	}
	return nil
}

// projectMarkdown formats a rich Markdown detail/report document for one project.
func (t *Tool) projectMarkdown(e Entry, st *RepoStatus) string {
	dir := t.Dir(e)
	lic := licenseOf(dir)
	lnk := t.linkedIn(t.DefaultHub(), e.Name, dir)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", e.Name)
	if e.Archived {
		b.WriteString("> **Archived** — hidden from default lists (`a` toggles visibility, `x` restores)\n\n")
	}
	b.WriteString("## Manifest\n\n")
	fmt.Fprintf(&b, "- **Path:** `%s`\n", e.Path)
	fmt.Fprintf(&b, "- **Directory:** `%s`\n", dir)
	if e.URL != "" {
		fmt.Fprintf(&b, "- **Origin URL:** `%s`\n", e.URL)
	}
	if e.Branch != "" {
		fmt.Fprintf(&b, "- **Recorded Branch:** `%s`\n", e.Branch)
	}
	if e.Origin != "" {
		fmt.Fprintf(&b, "- **Origin:** %s\n", e.Origin)
	}
	fmt.Fprintf(&b, "- **License:** %s\n", firstNonEmpty(lic, "unknown"))
	fmt.Fprintf(&b, "- **Hub Link:** %s (`%s`)\n", firstNonEmpty(lnk, "none"), t.tilde(filepath.Join(t.expand(t.DefaultHub()), e.Name)))

	if st != nil {
		b.WriteString("\n## Git Status\n\n")
		if st.Error != "" {
			fmt.Fprintf(&b, "- **State:** `%s` — %s\n", st.Status, st.Error)
		} else {
			fmt.Fprintf(&b, "- **State:** `%s`\n", formatStatusBadge(*st))
			if st.Branch != "" {
				fmt.Fprintf(&b, "- **Branch:** `%s`", st.Branch)
				if st.Upstream != "" {
					fmt.Fprintf(&b, " → `%s` (ahead %d, behind %d)", st.Upstream, st.Ahead, st.Behind)
				}
				b.WriteByte('\n')
			}
			fmt.Fprintf(&b, "- **Tracked files:** %d\n", st.Tracked)
			fmt.Fprintf(&b, "- **Changed:** %d (staged %d · unstaged %d · untracked %d · conflicts %d)\n",
				st.Changed, st.Staged, st.Unstaged, st.Untracked, st.Conflicts)
		}
	}

	// Include a short README preview if present on disk.
	if data, err := os.ReadFile(filepath.Join(dir, "README.md")); err == nil && len(data) > 0 {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 12 {
			lines = append(lines[:12], "", "...")
		}
		b.WriteString("\n## README.md\n\n")
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteByte('\n')
	}

	return strings.TrimRight(b.String(), "\n")
}

func newShell(u *UI) (*otea.Shell, tea.Cmd) {
	p := Page{
		Match:        u.Match,
		Ratio:        u.Ratio,
		Detail:       u.Detail,
		Density:      u.Density,
		ShowArchived: u.ShowArchived,
		Sort:         "name",
		MD:           markdown.New("Detail", "_Loading..._"),
	}
	p.Grid = *table.New(gridColumns, nil)
	if len(u.Notes) > 0 {
		p.warn(strings.Join(u.Notes, " · "))
	}
	initial := core.NewState(core.Frame{ID: pageID, FocusID: nodeGrid, Mode: "normal", Data: p})
	sh := otea.New(newEngine(u), initial, u.view, u.theme(&p))
	sh.OnPassThrough = func(e core.PassToWidgetEffect) tea.Cmd {
		if e.Mouse == nil || e.NodeID != nodeGrid {
			return nil
		}
		id := map[string]string{"wheeldown": "motion.down", "wheelup": "motion.up"}[e.Mouse.Button]
		if id == "" {
			return nil
		}
		return func() tea.Msg { return core.CommandEvent{Command: core.Command{ID: id, Count: 3, Source: "mouse"}} }
	}
	sh.OnCustom = func(e core.CustomEffect) tea.Cmd {
		switch d := e.Data.(type) {
		case core.Command:
			if e.Kind == "dispatch" {
				return func() tea.Msg { return core.CommandEvent{Command: d} }
			}
		case ui.Theme:
			sh.Theme = d
		}
		return nil
	}
	_, cmd := sh.Update(core.CommandEvent{Command: core.Command{ID: "project.refresh", Source: "startup", Actor: core.Actor{Kind: "system"}}})
	return sh, cmd
}

type startupModel struct {
	*otea.Shell
	cmd tea.Cmd
}

func (s startupModel) Init() tea.Cmd { return s.cmd }

func (t *Tool) runTUI(args []string) error {
	cfg := UIConfig{}
	for _, a := range args {
		if a == "--all" || a == "--archived" {
			cfg.ShowArchived = true
		}
	}
	sh, cmd := newShell(newUI(t, cfg))
	_, err := tea.NewProgram(startupModel{sh, cmd}).Run()
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}
