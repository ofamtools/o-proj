package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProjectCellsAndLicenseLinked(t *testing.T) {
	tl, _, _ := tool(t)
	hub := filepath.Join(tl.Home, "hub", "o")
	setEnv(tl, "O_PROJ_HUB", hub)
	mk := func(name, license string) string {
		dir := filepath.Join(tl.Home, "proj", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if license != "" {
			if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte(license), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	a := mk("a", "\nMIT License\n\nCopyright\n")
	mk("b", "Apache License\n")
	mk("c", "")
	if err := os.MkdirAll(filepath.Join(hub, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, filepath.Join(hub, "a")); err != nil {
		t.Fatal(err)
	}
	entries := []Entry{
		{Name: "a", Path: "~/proj/a", Origin: OriginNew},
		{Name: "b", Path: "~/proj/b", Origin: OriginAdopted},
		{Name: "c", Path: "~/proj/c"},
		{Name: "gone", Path: "~/proj/gone", Origin: OriginAdopted},
	}
	want := map[string][]string{
		"a":    {"a", "o-proj", "~/proj/a", "mit", "yes"},
		"b":    {"b", "adopted", "~/proj/b", "other", "other"},
		"c":    {"c", "", "~/proj/c", "none", "no"},
		"gone": {"gone", "adopted", "~/proj/gone", "", "no"},
	}
	for _, r := range tl.rows(entries) {
		got := tl.projectCells(r)
		if !slices.Equal(got, want[r.Name]) {
			t.Fatalf("%s cells %q, want %q", r.Name, got, want[r.Name])
		}
	}
}

func TestTemplateCells(t *testing.T) {
	tl, _, _ := tool(t)
	catalog(t, tl)
	tpls, err := tl.Templates()
	if err != nil {
		t.Fatal(err)
	}
	dir := tl.tilde(tl.TemplateDir())
	want := [][]string{
		{"packed", "tar.gz", dir + "/packed.tar.gz"},
		{"plain", "dir", dir + "/plain"},
	}
	if len(tpls) != 2 {
		t.Fatalf("templates %+v", tpls)
	}
	for i, tpl := range tpls {
		if got := tl.templateCells(tpl); !slices.Equal(got, want[i]) {
			t.Fatalf("tpl %d cells %q, want %q", i, got, want[i])
		}
	}
}
