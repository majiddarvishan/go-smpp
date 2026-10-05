package smpp_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The README states a supply-chain posture: this module has zero third-party
// dependencies, imports no unsafe, and uses no cgo. A claim like that is only
// worth publishing if it is checked, so this test enforces it on every Go file
// in the module, test files, examples and commands included. It complements the
// CI step that bans unsafe, and runs under a plain `go test ./...`.
//
// Scope, stated precisely because it is easy to overstate: it is about this
// module's own sources. The Go standard library and runtime use unsafe and cgo
// internally; the claim is that this module adds none of its own and pulls in
// nothing beyond the standard library.
func TestSupplyChainPosture(t *testing.T) {
	module := strings.TrimSuffix(modulePath, "/")
	var scanned int
	topDirs := map[string]bool{}
	sawStdlibTLS := false
	var violations []string

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != "." && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		scanned++
		if dir := strings.SplitN(filepath.ToSlash(path), "/", 2); len(dir) == 2 {
			topDirs[dir[0]] = true
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			switch {
			case p == "unsafe":
				violations = append(violations, path+": imports unsafe")
			case p == "C":
				violations = append(violations, path+": uses cgo (import \"C\")")
			case p == module || strings.HasPrefix(p, module+"/"):
				// this module
			case !strings.Contains(strings.SplitN(p, "/", 2)[0], "."):
				if p == "crypto/tls" {
					sawStdlibTLS = true
				}
			default:
				violations = append(violations, path+": imports third-party package "+p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Refuse to pass vacuously: the walk must actually have covered the module.
	if scanned < 50 {
		t.Fatalf("only %d Go files scanned; the walker is broken, not the module", scanned)
	}
	for _, dir := range []string{"client", "server", "session", "codec", "protocol", "transport", "message", "encoding", "cmd", "examples", "internal"} {
		if !topDirs[dir] {
			t.Fatalf("directory %q was not scanned", dir)
		}
	}
	if !sawStdlibTLS {
		t.Fatal("never saw the crypto/tls import; import classification may be broken")
	}

	// go.mod must declare no dependencies at all, and there must be no go.sum.
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(mod), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "require", "replace", "exclude", "retract":
			violations = append(violations, "go.mod: has a "+fields[0]+" directive: "+strings.TrimSpace(line))
		}
	}
	if info, err := os.Stat("go.sum"); err == nil && info.Size() > 0 {
		violations = append(violations, "go.sum exists and is not empty; the module has no dependencies to sum")
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("supply-chain posture violated:\n  %s", strings.Join(violations, "\n  "))
	}
}
