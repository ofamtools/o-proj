package main

import (
	"fmt"

	"ofam.dev/ochazuke"
)

// The project noun is a record noun over the manifest (noun scaffold): get,
// list, create (project new), set, archive, restore and delete are the
// standard verbs over these methods, and the named verbs (new, adopt, link,
// ...) stay beside them. The key and the label are the repository's name;
// the name is never rewritten (hub links and clones follow it). archive hides
// a row from project list and writes nothing else; delete removes the manifest
// entry only: the directory, its hub link and the GitHub repository stay.

func projectProps() []ochazuke.PropSpec {
	text := func(name, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, Parameter: ochazuke.Parameter{Type: "string", Description: desc}}
	}
	derived := func(name, typ, desc string) ochazuke.PropSpec {
		return ochazuke.PropSpec{Name: name, ReadOnly: true, Parameter: ochazuke.Parameter{Type: typ, Description: desc}}
	}
	return []ochazuke.PropSpec{
		{Name: "id", Key: true, CreateOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The repository's name (a row id project/NAME names it too)"}},
		{Name: "name", Human: true, Role: ochazuke.RoleLabel, CreateOnly: true, Parameter: ochazuke.Parameter{Type: "string", Description: "The repository's name in the manifest and under the hub; fixed once recorded"}},
		text("url", "The origin remote's URL"),
		{Name: "path", Parameter: ochazuke.Parameter{Type: ochazuke.PropPath, Description: "The repository's directory, with ~ for the home directory"}},
		text("branch", "The branch recorded when the repository was added"),
		text("origin", "o-proj when o-proj made it, adopted when it was recorded from elsewhere"),
		{Name: "archived", Role: ochazuke.RoleArchive, Parameter: ochazuke.Parameter{Type: ochazuke.SettingBoolean, Description: "Hidden from project list; nothing else changes"}},
		derived("dir", ochazuke.PropPath, "The directory on this machine (the path expanded)"),
		derived("license", "string", "mit, other, none, or empty when the directory is not on this machine"),
		derived("linked", "string", "yes, no or other: whether the hub links the name to the directory"),
	}
}

// projectThing is an entry as the store's thing.
func (t *Tool) projectThing(e Entry) ochazuke.Thing {
	dir := t.Dir(e)
	return ochazuke.Thing{
		"id": e.Name, "name": e.Name, "url": e.URL, "path": e.Path, "branch": e.Branch, "origin": e.Origin, "archived": e.Archived,
		"dir": dir, "license": licenseOf(dir), "linked": t.linkedIn(t.DefaultHub(), e.Name, dir),
	}
}

func thingString(t ochazuke.Thing, key string) (string, bool) {
	v, ok := t[key]
	if !ok || v == nil {
		return "", false
	}
	return fmt.Sprint(v), true
}

func thingBool(t ochazuke.Thing, key string) (bool, bool) {
	switch v := t[key].(type) {
	case bool:
		return v, true
	case string:
		return v == "true", true
	}
	return false, false
}

func (t *Tool) projectStore() ochazuke.Store {
	return ochazuke.Store{
		Kind: ochazuke.KindCollection,
		Get: func(_ ochazuke.Ctx, id string) (ochazuke.Thing, error) {
			entries, err := t.Manifest()
			if err != nil {
				return nil, err
			}
			e, ok := find(entries, id)
			if !ok {
				return nil, fmt.Errorf("no repository %q in %s; see project list", id, t.tilde(t.ManifestPath()))
			}
			return t.projectThing(e), nil
		},
		List: func(ochazuke.Ctx, ochazuke.ListQuery) ([]ochazuke.Thing, error) {
			entries, err := t.Manifest()
			if err != nil {
				return nil, err
			}
			out := make([]ochazuke.Thing, len(entries))
			for i, e := range entries {
				out[i] = t.projectThing(e)
			}
			return out, nil
		},
		Create: func(_ ochazuke.Ctx, th ochazuke.Thing) (ochazuke.Thing, error) {
			name, _ := thingString(th, "name")
			args := ochazuke.Arguments{"name": name}
			for _, k := range []string{"template", "root", "license", "hub"} {
				if v, ok := thingString(th, k); ok && v != "" {
					args[k] = v
				}
			}
			if _, err := t.newFromArgs(args); err != nil {
				return nil, err
			}
			entries, err := t.Manifest()
			if err != nil {
				return nil, err
			}
			e, _ := find(entries, name)
			return t.projectThing(e), nil
		},
		Patch: func(_ ochazuke.Ctx, id string, th ochazuke.Thing) (ochazuke.Thing, error) {
			entries, err := t.Manifest()
			if err != nil {
				return nil, err
			}
			i := -1
			for j, e := range entries {
				if e.Name == id {
					i = j
				}
			}
			if i < 0 {
				return nil, fmt.Errorf("no repository %q in %s; see project list", id, t.tilde(t.ManifestPath()))
			}
			e := &entries[i]
			for key, field := range map[string]*string{"url": &e.URL, "path": &e.Path, "branch": &e.Branch, "origin": &e.Origin} {
				if v, ok := thingString(th, key); ok {
					*field = v
				}
			}
			if v, ok := thingBool(th, "archived"); ok {
				e.Archived = v
			}
			if err := t.SaveManifest(entries); err != nil {
				return nil, err
			}
			return t.projectThing(*e), nil
		},
		Delete: func(_ ochazuke.Ctx, id string) error {
			entries, err := t.Manifest()
			if err != nil {
				return err
			}
			kept := entries[:0:0]
			for _, e := range entries {
				if e.Name != id {
					kept = append(kept, e)
				}
			}
			if len(kept) == len(entries) {
				return fmt.Errorf("no repository %q in %s; see project list", id, t.tilde(t.ManifestPath()))
			}
			return t.SaveManifest(kept)
		},
		Confirm: map[string]string{"delete": "Remove {{label}} from the manifest? (its directory, its hub link and any GitHub repository stay)"},
	}
}
