package smpp_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/majiddarvishan/go-smpp/"

var allowedInternalImports = map[string]map[string]bool{
	"protocol":  {},
	"encoding":  {},
	"transport": {},
	"codec":     {"protocol": true},
	"message":   {"protocol": true, "encoding": true},
	"session":   {"protocol": true, "codec": true, "transport": true},
	"client":    {"protocol": true, "session": true, "transport": true, "message": true},
	"server":    {"protocol": true, "session": true, "transport": true, "message": true},
}

func TestPackageDependencyDirection(t *testing.T) {
	for pkg, allowed := range allowedInternalImports {
		pkg := pkg
		allowed := allowed
		t.Run(pkg, func(t *testing.T) {
			entries, err := os.ReadDir(pkg)
			if err != nil {
				t.Fatalf("read package %q: %v", pkg, err)
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
					continue
				}
				path := filepath.Join(pkg, entry.Name())
				f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatalf("parse %s: %v", path, err)
				}
				for _, imp := range f.Imports {
					checkImport(t, pkg, allowed, imp)
				}
			}
		})
	}
}

func checkImport(t *testing.T, pkg string, allowed map[string]bool, imp *ast.ImportSpec) {
	t.Helper()
	path, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		t.Fatalf("unquote import %s: %v", imp.Path.Value, err)
	}
	if !strings.HasPrefix(path, modulePath) {
		return
	}
	rest := strings.TrimPrefix(path, modulePath)
	target := strings.Split(rest, "/")[0]
	if target == pkg {
		return
	}
	if !allowed[target] {
		t.Fatalf("package %q must not import higher/disallowed internal package %q (%s)", pkg, target, path)
	}
}
