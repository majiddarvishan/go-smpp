package smpp_test

import (
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestExportedAPISnapshot is the exhaustive check behind docs/API_COMPATIBILITY.md.
// api_contract_test.go pins a sample of 28 identifiers; this pins all of them.
//
// For every public package it type-checks the source with the standard library only,
// writes one line per exported thing (constants with their values, variables, functions,
// types, the exported fields of structs, the methods of interfaces and of every named
// type, and whether an interface is sealed), and compares that with the committed file
// in testdata/api. Any difference fails the test:
//
//   - a line only in the committed file was REMOVED or CHANGED: a breaking change, which
//     is a compatibility decision before it is a code change;
//   - a line only in the current API was ADDED: allowed, but it has to be acknowledged so
//     a reviewer sees it.
//
// After a deliberate change, accept it with:
//
//	go test -run TestExportedAPISnapshot -update .
//
// Unexported identifiers do not appear, so changing them never fails this test.
var updateAPI = flag.Bool("update", false, "rewrite testdata/api/*.txt from the current exported API")

// Directories that are not public packages.
var notPublic = map[string]bool{"cmd": true, "examples": true, "internal": true, "docs": true, "scripts": true, "testdata": true, "vendor": true}

func TestExportedAPISnapshot(t *testing.T) {
	// go/types keeps aliases as aliases from Go 1.23; pin that so the snapshot reads the
	// same whatever the go line says.
	t.Setenv("GODEBUG", strings.Trim(os.Getenv("GODEBUG")+",gotypesalias=1", ","))

	dirs := publicPackageDirs(t)
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil) // shared, so each dependency is checked once
	module := moduleImportPath(t)

	for _, dir := range dirs {
		lines := apiLines(t, fset, imp, module, dir)
		golden := filepath.Join("testdata", "api", dir+".txt")
		want, err := os.ReadFile(golden)
		if *updateAPI {
			if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(golden, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err != nil {
			t.Errorf("package %s has no snapshot (%v). If it is a new public package, run: go test -run TestExportedAPISnapshot -update .", dir, err)
			continue
		}
		removed, added := diffLines(strings.Split(strings.TrimRight(string(want), "\n"), "\n"), lines)
		if len(removed) == 0 && len(added) == 0 {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "the exported API of package %s no longer matches %s.\n", dir, golden)
		if len(removed) > 0 {
			fmt.Fprintf(&b, "\nREMOVED or CHANGED (breaking, needs a compatibility decision):\n")
			for _, l := range cap40(removed) {
				fmt.Fprintf(&b, "  - %s\n", l)
			}
		}
		if len(added) > 0 {
			fmt.Fprintf(&b, "\nADDED (allowed, but acknowledge it):\n")
			for _, l := range cap40(added) {
				fmt.Fprintf(&b, "  + %s\n", l)
			}
		}
		b.WriteString("\nIf this is deliberate, run: go test -run TestExportedAPISnapshot -update .  and commit the new file.")
		t.Error(b.String())
	}

	if *updateAPI {
		return
	}
	// a snapshot for a package that no longer exists is a stale promise
	entries, _ := os.ReadDir(filepath.Join("testdata", "api"))
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".txt")
		found := false
		for _, d := range dirs {
			if d == name {
				found = true
			}
		}
		if !found {
			t.Errorf("testdata/api/%s is a snapshot of a package that is no longer public; remove it deliberately", e.Name())
		}
	}
}

func cap40(s []string) []string {
	if len(s) > 40 {
		return append(append([]string{}, s[:40]...), fmt.Sprintf("... and %d more", len(s)-40))
	}
	return s
}

func diffLines(want, got []string) (removed, added []string) {
	w, g := map[string]bool{}, map[string]bool{}
	for _, l := range want {
		w[l] = true
	}
	for _, l := range got {
		g[l] = true
	}
	for _, l := range want {
		if !g[l] {
			removed = append(removed, l)
		}
	}
	for _, l := range got {
		if !w[l] {
			added = append(added, l)
		}
	}
	return
}

func moduleImportPath(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
			return f[1]
		}
	}
	t.Fatal("no module line in go.mod")
	return ""
}

// publicPackageDirs returns every top-level directory that holds a Go package and is not
// one of the non-public ones, so a new public package cannot be added without a snapshot.
func publicPackageDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || notPublic[e.Name()] {
			continue
		}
		if bp, err := build.ImportDir(e.Name(), 0); err != nil || len(bp.GoFiles) == 0 {
			continue
		}
		dirs = append(dirs, e.Name())
	}
	sort.Strings(dirs)
	if len(dirs) < 8 {
		t.Fatalf("found only %v as public packages; the directory scan is broken", dirs)
	}
	return dirs
}

func apiLines(t *testing.T, fset *token.FileSet, imp types.Importer, module, dir string) []string {
	t.Helper()
	bp, err := build.ImportDir(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, name := range bp.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(module+"/"+dir, fset, files, nil)
	if err != nil {
		t.Fatalf("type-checking %s: %v", dir, err)
	}
	q := func(p *types.Package) string {
		if p == pkg {
			return ""
		}
		return p.Name()
	}

	type entry struct{ key, text string }
	var all []entry
	add := func(name, member, text string) { all = append(all, entry{name + "\x00" + member, text}) }

	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch o := obj.(type) {
		case *types.Const:
			add(name, "", fmt.Sprintf("const %s %s = %s", name, types.TypeString(o.Type(), q), o.Val().ExactString()))
		case *types.Var:
			add(name, "", fmt.Sprintf("var %s %s", name, types.TypeString(o.Type(), q)))
		case *types.Func:
			add(name, "", types.ObjectString(o, q))
		case *types.TypeName:
			if o.IsAlias() {
				add(name, "", fmt.Sprintf("type %s = %s", name, types.TypeString(o.Type(), q)))
				continue
			}
			named, ok := o.Type().(*types.Named)
			if !ok {
				add(name, "", types.ObjectString(o, q))
				continue
			}
			if named.TypeParams().Len() > 0 {
				add(name, "", types.ObjectString(o, q))
			}
			switch u := named.Underlying().(type) {
			case *types.Struct:
				add(name, "", "type "+name+" struct")
				for i := 0; i < u.NumFields(); i++ {
					if f := u.Field(i); f.Exported() {
						emb := ""
						if f.Embedded() {
							emb = " (embedded)"
						}
						add(name, "1"+f.Name(), fmt.Sprintf("field %s.%s %s%s", name, f.Name(), types.TypeString(f.Type(), q), emb))
					}
				}
			case *types.Interface:
				add(name, "", "type "+name+" interface")
				sealed := false
				for i := 0; i < u.NumMethods(); i++ {
					m := u.Method(i)
					if !m.Exported() {
						sealed = true
						continue
					}
					add(name, "1"+m.Name(), fmt.Sprintf("imethod %s.%s%s", name, m.Name(), strings.TrimPrefix(types.TypeString(m.Type(), q), "func")))
				}
				if sealed {
					add(name, "2sealed", "sealed "+name+" (has unexported methods, so it cannot be implemented outside the package)")
				}
			default:
				if named.TypeParams().Len() == 0 {
					add(name, "", fmt.Sprintf("type %s %s", name, types.TypeString(u, q)))
				}
			}
			if _, isIface := named.Underlying().(*types.Interface); !isIface {
				ms := types.NewMethodSet(types.NewPointer(named))
				for i := 0; i < ms.Len(); i++ {
					fn, ok := ms.At(i).Obj().(*types.Func)
					if !ok || !fn.Exported() {
						continue
					}
					add(name, "3"+fn.Name(), "method "+strings.TrimPrefix(types.ObjectString(fn, q), "func "))
				}
			}
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].key < all[j].key })
	lines := []string{fmt.Sprintf("# Exported API of %s/%s. Regenerate with: go test -run TestExportedAPISnapshot -update .", module, dir)}
	for _, e := range all {
		lines = append(lines, e.text)
	}
	return lines
}
