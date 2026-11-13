package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The manifest is o proj's own record of the family's repositories:
// $HOME/hub/repos.json ($O_PROJ_MANIFEST), a JSON array sorted by name. Paths
// are stored with ~ for the home directory and expanded per machine, so
// the same file works wherever the hub is copied; the hub links, clone and
// sync are generated from it.

// Origins: o-proj made the repository (new), or it was recorded from
// elsewhere (adopt, seed). Provenance only; nothing filters on it.
const (
	OriginNew     = "o-proj"
	OriginAdopted = "adopted"
)

// Entry is one repository in the manifest.
type Entry struct {
	Name   string `json:"name"`
	URL    string `json:"url,omitempty"`
	Path   string `json:"path"` // ~ for the home directory
	Branch string `json:"branch,omitempty"`
	Origin string `json:"origin,omitempty"`
	// Archived hides the repository from project lists (project archive,
	// project restore); sync, clone and link still treat it as recorded.
	Archived bool `json:"archived,omitempty"`
}

// Row is an entry as list prints it: the entry plus its path expanded for
// this machine.
type Row struct {
	Entry
	Dir string `json:"dir"`
}

// ManifestPath is $O_PROJ_MANIFEST, else $HOME/hub/repos.json.
func (t *Tool) ManifestPath() string {
	if v := lookup(t.Env, "O_PROJ_MANIFEST"); v != "" {
		return t.expand(v)
	}
	return filepath.Join(t.Home, "hub", "repos.json")
}

// Manifest reads the manifest; a missing file is empty.
func (t *Tool) Manifest() ([]Entry, error) {
	path := t.ManifestPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", t.tilde(path), err)
	}
	return entries, nil
}

// SaveManifest writes the manifest sorted by name, through a temporary file
// renamed into place so a failed write never leaves half a file.
func (t *Tool) SaveManifest(entries []Entry) error {
	entries = append([]Entry(nil), entries...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	if entries == nil {
		entries = []Entry{}
	}
	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	path := t.ManifestPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".repos-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(out, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Dir is an entry's path on this machine.
func (t *Tool) Dir(e Entry) string { return t.expand(e.Path) }

// rows are the entries with their directories.
func (t *Tool) rows(entries []Entry) []Row {
	out := make([]Row, 0, len(entries))
	for _, e := range entries {
		out = append(out, Row{Entry: e, Dir: t.Dir(e)})
	}
	return out
}

// find is the entry named name.
func find(entries []Entry, name string) (Entry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// pick is the named entries (all of them when names is empty); an
// unknown name is an error.
func (t *Tool) pick(entries []Entry, names []string) ([]Entry, error) {
	if len(names) == 0 {
		return entries, nil
	}
	var out []Entry
	for _, n := range names {
		e, ok := find(entries, n)
		if !ok {
			return nil, fmt.Errorf("no repository %q in %s; see %s list", n, t.tilde(t.ManifestPath()), t.Prog)
		}
		out = append(out, e)
	}
	return out, nil
}

// describe is the entry the manifest records for the repository at dir:
// its origin remote and current branch, read from git; either is empty
// when git has none.
func (t *Tool) describe(name, dir, origin string) Entry {
	e := Entry{Name: name, Path: t.tilde(dir), Origin: origin}
	if out, err := t.Run(dir, "git", "remote", "get-url", "origin"); err == nil {
		e.URL = strings.TrimSpace(string(out))
	}
	if out, err := t.Run(dir, "git", "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		e.Branch = strings.TrimSpace(string(out))
	}
	return e
}

// record adds e to the manifest, or reports the entry already there: an
// existing name is never rewritten (a second new --existing, or adopt of a
// recorded directory, keeps the first entry). The outcome is recorded,
// exists (same directory) or taken (another directory).
func (t *Tool) record(e Entry) (string, error) {
	entries, err := t.Manifest()
	if err != nil {
		return "", err
	}
	if old, ok := find(entries, e.Name); ok {
		if samePath(t.Dir(old), t.Dir(e)) {
			t.say("manifest: %s -> %s (exists)", e.Name, old.Path)
			return "exists", nil
		}
		t.say("manifest: keep %s -> %s (not %s)", e.Name, old.Path, e.Path)
		return "taken", nil
	}
	if err := t.SaveManifest(append(entries, e)); err != nil {
		return "", err
	}
	t.say("manifest: %s -> %s in %s", e.Name, e.Path, t.tilde(t.ManifestPath()))
	return "recorded", nil
}
