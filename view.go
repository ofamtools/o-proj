package main

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"ofam.dev/ochazuke/core"
	"ofam.dev/ochazuke/ui"
	"ofam.dev/ochazuke/widgets/markdown"
	"ofam.dev/ochazuke/widgets/picker"
	"ofam.dev/ochazuke/widgets/table"
)

type geom struct {
	tableW, tableH, rows int
	side                 bool
}

func (u *UI) geometry(p *Page, w, h int) geom {
	if w <= 0 || h <= 0 {
		w, h = 120, 32
	}
	body := h - 3
	if p.Typing {
		body--
	}
	body = max(body, 3)
	g := geom{tableW: w, tableH: body}
	if p.Detail {
		if w >= u.StackW {
			g.side = true
			g.tableW = int(math.Round(float64(max(0, w-1)) * p.Ratio))
		} else {
			g.tableH = int(math.Round(float64(max(0, body-1)) * p.Ratio))
		}
	}
	g.rows = max(g.tableH-2, 1)
	return g
}

func textW(s string) int { return utf8.RuneCountInString(s) }

func textNode(t, tone string) ui.ViewNode {
	return ui.ViewNode{Kind: ui.KindText, Tone: tone, Props: map[string]any{"text": t}}
}

func clip(s string, w int) string {
	if r := []rune(s); w > 1 && len(r) > w {
		return string(r[:w-1]) + "…"
	}
	return s
}

func (u *UI) view(s core.State) ui.ViewNode {
	p := pageOf(s)
	w := s.Width
	if w <= 0 {
		w = 120
	}
	g := u.geometry(&p, s.Width, s.Height)
	grid := p.Grid.Copy()
	grid.PageSize = g.rows
	grid.Columns = make([]table.Column, len(gridColumns))
	for i, c := range gridColumns {
		if c.Key == p.Sort {
			if p.Desc {
				c.Title += " ▼"
			} else {
				c.Title += " ▲"
			}
		}
		grid.Columns[i] = c
	}
	body := grid.View(nodeGrid)
	body.Title = fmt.Sprintf("Projects %d", len(p.base))
	if p.Detail {
		split := ui.ViewNode{ID: "body", Kind: ui.KindSplit, Ratio: p.Ratio, Gap: 1, Props: map[string]any{"axis": "horizontal"}}
		if !g.side {
			split.Props["axis"] = "vertical"
		}
		split.Children = []ui.ViewNode{body, u.detailNode(&p, &grid)}
		body = split
	}
	children := []ui.ViewNode{u.titleLine(&p, w), body}
	if p.Typing {
		children = append(children, p.inputNode())
	}
	children = append(children, p.infoLine(s, &grid, w), statusLine(&p, w))
	root := ui.ViewNode{ID: "root", Kind: ui.KindVStack, Children: []ui.ViewNode{
		{ID: "page", Kind: ui.KindVStack, Scope: "grid", Children: children},
	}}
	for _, m := range s.Modals {
		root.Children = append(root.Children, u.modalView(m))
	}
	return root
}

func (u *UI) titleLine(p *Page, w int) ui.ViewNode {
	s := p.Summary
	line := fmt.Sprintf(" o-proj · Projects %d · %d clean · %d dirty", len(p.Items), s.Clean, s.Dirty)
	if s.Errors > 0 {
		line += fmt.Sprintf(" · %d unavailable", s.Errors)
	}
	tone := "title"
	switch {
	case p.Loading:
		line += " · scanning…"
	case p.LoadErr != "":
		line += " · scan failed"
		tone = "error"
	}
	if p.ShowArchived {
		line += " · archived shown"
	}
	return textNode(clip(line, w), tone)
}

func (p *Page) inputNode() ui.ViewNode {
	line := p.QueryLine
	line.Sync(p.Query)
	return ui.ViewNode{
		ID: nodeFilter, Kind: ui.KindInput, Focusable: true,
		Title: "/ filter [" + string(p.Match) + "]",
		Props: map[string]any{
			"value":       p.Query,
			"cursor":      line.Cursor,
			"placeholder": "type to filter · enter keeps · esc clears · tab match mode · ↑↓ move · ctrl+a/e/w/u/k edit",
		},
	}
}

func (p *Page) infoLine(s core.State, grid *table.Model, w int) ui.ViewNode {
	var parts []string
	items := grid.Items()
	pos := "no rows"
	if len(items) > 0 {
		pos = fmt.Sprintf("row %d/%d", grid.SelectedIdx+1, len(items))
	}
	if len(grid.Rows) != len(p.base) {
		pos += fmt.Sprintf(" (%d of %d match)", len(grid.Rows), len(p.base))
	}
	dir := "▲"
	if p.Desc {
		dir = "▼"
	}
	parts = append(parts, pos, "sort "+p.Sort+" "+dir)
	if n := len(p.Grid.Marked); n > 0 {
		parts = append(parts, fmt.Sprintf("%d marked", n))
	}
	tone := "muted"
	if p.Query != "" || p.Typing {
		parts = append(parts, fmt.Sprintf("filter %s %q", p.Match, p.Query))
	}
	if p.FilterErr != "" {
		parts = append(parts, "regex error: "+p.FilterErr)
		tone = "error"
	}
	if p.Grid.GroupField != "" {
		parts = append(parts, "grouped by "+p.Grid.GroupField)
	}
	if in := s.Input; in.Count != nil || len(in.Prefix) > 0 {
		pre := strings.Join(in.Prefix, " ")
		if in.Count != nil {
			pre = strconv.Itoa(*in.Count) + " " + pre
		}
		parts = append(parts, "pending "+strings.TrimSpace(pre))
	}
	return textNode(clip(" "+strings.Join(parts, " · "), w), tone)
}

func statusLine(p *Page, w int) ui.ViewNode {
	if p.Status != "" {
		return textNode(clip(" "+strings.ReplaceAll(p.Status, "\n", " "), w), p.Tone)
	}
	hint := " ? help · : commands · / filter · o/O sort · g b group · n new · e edit · A adopt · l/u link · s sync · x archive · d d remove · r refresh · q quit"
	return textNode(clip(hint, w), "muted")
}

func (u *UI) detailNode(p *Page, _ *table.Model) ui.ViewNode {
	md := p.MD
	if md == nil {
		md = markdown.New("Detail", "_No project selected._")
	}
	cp := *md
	cp.Offset = p.DetailOff
	return cp.View(nodeDetail)
}

func windowLines(body string, off int) string {
	lines := strings.Split(body, "\n")
	off = min(max(off, 0), len(lines))
	return strings.Join(lines[off:], "\n")
}

func (u *UI) modalView(f core.Frame) ui.ViewNode {
	switch f.ID {
	case modalPalette:
		m, _ := f.Data.(*picker.Model)
		if m == nil {
			break
		}
		return ui.ViewNode{
			ID: "palette.overlay", Kind: ui.KindOverlay, Scope: "palette",
			Title: "Command palette · " + string(m.MatchMode),
			Props: map[string]any{"widthPct": 70, "heightPct": 70},
			Children: []ui.ViewNode{
				m.View(modalPalette),
				textNode("enter runs · esc closes · tab match mode · ↑↓ move · ctrl+a/e/w/u/k edit", "muted"),
			},
		}
	case modalHelp:
		off, _ := f.Data.(int)
		return ui.ViewNode{
			ID: modalHelp, Kind: ui.KindOverlay, Scope: "help", Focusable: true,
			Title: "Help · active key bindings",
			Props: map[string]any{"widthPct": 80, "heightPct": 85},
			Children: []ui.ViewNode{
				textNode(windowLines(strings.Join(u.helpLines(), "\n"), off), "text"),
			},
		}
	case modalConfirm:
		d, _ := f.Data.(confirmData)
		return ui.ViewNode{
			ID: modalConfirm, Kind: ui.KindOverlay, Scope: "confirm", Focusable: true,
			Title: d.Title,
			Props: map[string]any{"widthPct": 70, "heightPct": 60},
			Children: []ui.ViewNode{
				textNode(windowLines(d.Text, d.Off), "text"),
				textNode("y confirms · n / esc cancels", "warning"),
			},
		}
	case modalForm:
		fd, ok := f.Data.(formData)
		if !ok || fd.M == nil {
			break
		}
		kids := []ui.ViewNode{fd.M.View(modalForm)}
		for _, sp := range fd.M.Specs {
			if e := fd.M.Errors[sp.Name]; e != "" {
				kids = append(kids, textNode(sp.Name+": "+e, "error"))
			}
		}
		kids = append(kids, textNode("type to edit (ctrl+a/e/w/u/k) · space toggles bool/enum · tab/↑↓ field · enter submits · esc cancels", "muted"))
		return ui.ViewNode{
			ID: "form.overlay", Kind: ui.KindOverlay, Scope: "form",
			Title:    fd.M.Title,
			Props:    map[string]any{"widthPct": 75, "heightPct": 75},
			Children: kids,
		}
	}
	return ui.ViewNode{}
}

func (u *UI) helpLines() []string {
	descByID := map[string]string{}
	for _, sp := range u.Specs {
		descByID[sp.ID] = sp.Description
	}
	var lines []string
	lines = append(lines, "Normal mode bindings:")
	for _, bd := range u.Bindings {
		if bd.Mode != "normal" || bd.Hidden || len(bd.Keys) == 0 {
			continue
		}
		keys := strings.Join(bd.Keys, " ")
		desc := descByID[bd.Command]
		if desc == "" {
			desc = bd.Hint
		}
		lines = append(lines, fmt.Sprintf("  %-12s  %-26s  %s", keys, bd.Command, desc))
	}
	lines = append(lines, "", "Input / Form / Palette Readline bindings:")
	seen := map[string]bool{}
	for _, bd := range u.Bindings {
		if bd.Mode != "input" || bd.Hidden || len(bd.Keys) == 0 {
			continue
		}
		k := strings.Join(bd.Keys, " ")
		if seen[k] {
			continue
		}
		seen[k] = true
		lines = append(lines, fmt.Sprintf("  %-12s  %-26s  %s", k, bd.Command, descByID[bd.Command]))
	}
	return slices.Clip(lines)
}
