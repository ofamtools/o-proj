package main

import (
	"encoding/json"
	"testing"

	"ofam.dev/ochazuke"
	ocli "ofam.dev/ochazuke/cli"
)

// Every verb o-proj defines declares its history kind (Operation.HistoryKind)
// rather than leaving it to ochazuke's default, and the catalog's history is
// that kind. A new verb without one fails here. The framework's own verbs (the
// catalog's builtin) are left to ochazuke, so a library release that adds some
// does not change the count.
func TestEveryVerbDeclaresItsHistory(t *testing.T) {
	tl, out, _ := tool(t)
	if err := tl.Main([]string{"catalog"}); err != nil {
		t.Fatal(err)
	}
	var doc ocli.CatalogDoc
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	sh, err := tl.shell()
	if err != nil {
		t.Fatal(err)
	}
	ops := sh.Runtime.Operations
	n := 0
	for _, noun := range doc.Nouns {
		for _, v := range noun.Verbs {
			key := noun.Name + "." + v.Name
			if v.Builtin {
				continue
			}
			n++
			if v.History == "" || ops.At(ochazuke.Key(key)).HistoryKind == "" || v.History != ops.At(ochazuke.Key(key)).HistoryKind {
				t.Errorf("catalog verb %s: history %q, declared %q", key, v.History, ops.At(ochazuke.Key(key)).HistoryKind)
			}
		}
	}
	// The 13 verbs the tool names, project create (project new under the core
	// name), and the standard verbs the two nouns get over their stores.
	if n != 35 {
		t.Fatalf("the catalog has %d o-proj verbs, want 35", n)
	}
	for key, want := range map[string]string{
		"project.list": ochazuke.HistoryFetch, "template.list": ochazuke.HistoryFetch,
		"project.new": ochazuke.HistoryChange, "project.create": ochazuke.HistoryChange, "project.sync": ochazuke.HistoryChange, "project.link": ochazuke.HistoryChange,
	} {
		if got := ops.At(ochazuke.Key(key)).HistoryKind; got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

func TestCatalogDeclaresSingularNamespace(t *testing.T) {
	var doc ocli.CatalogDoc
	if err := json.Unmarshal([]byte(catalogOut(t)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Namespace != "proj" || ochazuke.DefaultNamespace != "proj" {
		t.Fatalf("catalog namespace %q, default %q; want proj", doc.Namespace, ochazuke.DefaultNamespace)
	}
	if got, err := ocli.CatalogNamespace(doc, "o-proj"); err != nil || got != "proj" {
		t.Fatal(got, err)
	}
}

func catalogOut(t *testing.T) string {
	t.Helper()
	tl, out, _ := tool(t)
	if err := tl.Main([]string{"catalog"}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
