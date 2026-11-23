package main

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// projectCells are a manifest row's cells: name, origin, path (with ~),
// license (licenseOf) and linked (linkedIn the default hub).
func (t *Tool) projectCells(r Row) []string {
	return []string{r.Name, r.Origin, r.Path, licenseOf(r.Dir), t.linkedIn(t.DefaultHub(), r.Name, r.Dir)}
}

// templateCells are a template's cells: name, kind, path (with ~).
func (t *Tool) templateCells(tpl Template) []string {
	return []string{tpl.Name, tpl.Kind, t.tilde(tpl.Path)}
}

// licenseOf names the license in dir: mit for an MIT LICENSE, other for
// any other LICENSE, LICENSE.md, LICENSE.txt or COPYING, none without one,
// and "" when dir is not on this machine.
func licenseOf(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	for _, name := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"} {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := strings.TrimSpace(strings.TrimLeft(s.Text(), "# "))
			if line == "" {
				continue
			}
			if strings.HasPrefix(strings.ToLower(line), "mit license") {
				return "mit"
			}
			break
		}
		return "other"
	}
	return "none"
}

// linkedIn is whether hub/name links to dir: yes, no (nothing there), or
// other (something else is at the name; link keeps it); "" when hub is
// NoHub.
func (t *Tool) linkedIn(hub, name, dir string) string {
	if hub == NoHub {
		return ""
	}
	path := filepath.Join(t.expand(hub), name)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "no"
	}
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return "other"
	}
	target, err := os.Readlink(path)
	if err == nil && (target == dir || samePath(target, dir)) {
		return "yes"
	}
	return "other"
}
