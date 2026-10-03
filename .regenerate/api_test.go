package regenerate

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tmc/go-iroh/key"

	irohacp "github.com/carsonfarmer/iroh-acp-go"
)

// specSection returns the text of the SPEC.md section whose heading starts
// with number, such as "5".
func specSection(t *testing.T, number string) string {
	t.Helper()
	spec, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatal(err)
	}
	for section := range strings.SplitSeq(string(spec), "\n## ") {
		if strings.HasPrefix(section, number+". ") {
			return section
		}
	}
	t.Fatalf("SPEC.md has no section %s", number)
	return ""
}

// exports lists the exported declarations in f, one per line and without
// parameter names. A package imported under another name is written by the
// last element of its path, so an implementation that imports
// github.com/tmc/go-iroh/key as irohkey still reads key.EndpointID.
func exports(f *ast.File) []string {
	qualifiers := map[string]string{}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); imp.Name != nil && token.IsIdentifier(path.Base(p)) {
			qualifiers[imp.Name.Name] = path.Base(p)
		}
	}
	format := func(x ast.Expr) string {
		ast.Inspect(x, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && qualifiers[id.Name] != "" {
					id.Name = qualifiers[id.Name]
				}
			}
			return true
		})
		ast.Inspect(x, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncType); ok {
				unname(fn.Params)
				unname(fn.Results)
			}
			return true
		})
		return types.ExprString(x)
	}
	var decls []string
	for _, decl := range f.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name.IsExported() {
				decls = append(decls, "func "+decl.Name.Name+strings.TrimPrefix(format(decl.Type), "func"))
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if spec.Name.IsExported() {
						decls = append(decls, "type "+spec.Name.Name+" "+format(spec.Type))
					}
				case *ast.ValueSpec:
					for i, name := range spec.Names {
						if !name.IsExported() {
							continue
						}
						d := decl.Tok.String() + " " + name.Name
						if spec.Type != nil {
							d += " " + format(spec.Type)
						}
						if i < len(spec.Values) {
							d += " = " + format(spec.Values[i])
						}
						decls = append(decls, d)
					}
				}
			}
		}
	}
	return decls
}

// unname drops the names from a parameter or result list and keeps one entry
// per value.
func unname(fields *ast.FieldList) {
	if fields == nil {
		return
	}
	var list []*ast.Field
	for _, f := range fields.List {
		for range max(1, len(f.Names)) {
			list = append(list, &ast.Field{Type: f.Type})
		}
	}
	fields.List = list
}

// TestAPI checks that the package exports exactly the declarations in the Go
// block of SPEC.md section 5, and that the constant and AllowIDs behave as
// the spec says.
func TestAPI(t *testing.T) {
	fset := token.NewFileSet()
	_, block, _ := strings.Cut(specSection(t, "5"), "```go\n")
	block, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("SPEC.md section 5 has no Go block")
	}
	f, err := parser.ParseFile(fset, "SPEC.md", "package irohacp\n"+block, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := exports(f)
	files, err := filepath.Glob(filepath.Join("..", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, exports(f)...)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("the package exports\n\t%s\nand SPEC.md section 5 says\n\t%s", strings.Join(got, "\n\t"), strings.Join(want, "\n\t"))
	}

	if irohacp.ALPN != "acp/1" {
		t.Errorf("ALPN = %q, want %q", irohacp.ALPN, "acp/1")
	}
	id := func(seed byte) key.EndpointID {
		sk, err := key.SecretKeyFromSlice(bytes.Repeat([]byte{seed}, key.SeedSize))
		if err != nil {
			t.Fatal(err)
		}
		return sk.Public().EndpointID()
	}
	a, b := id(1), id(2)
	if allow := irohacp.AllowIDs(a); !allow(a) || allow(b) {
		t.Error("AllowIDs must accept exactly the IDs it was given")
	}
	if irohacp.AllowIDs()(a) {
		t.Error("AllowIDs with no IDs must accept nobody")
	}
}

// TestFlags checks each command's -h output against the flag table in its
// section of SPEC.md: the names, the defaults and the Go usage text. It prints
// the table's flags with the flag package and expects the same lines.
func TestFlags(t *testing.T) {
	bin := build(t, "github.com/carsonfarmer/iroh-acp-go/cmd/...")
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `-([a-z]+)` \\| (.+?) \\| .+? \\| ``(.+?)`` \\|$")
	for number, cmd := range map[string]string{"6": "acp-server", "7": "acp-client"} {
		flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
		for _, m := range row.FindAllStringSubmatch(specSection(t, number), -1) {
			def := strings.Trim(m[2], "`")
			switch {
			case def == m[2]: // not code, as in "none, required"
				def = ""
			case strings.HasPrefix(def, "<config dir>/"):
				def = filepath.Join(configDir, strings.TrimPrefix(def, "<config dir>/"))
			}
			flags.String(m[1], def, m[3])
		}
		var want strings.Builder
		flags.SetOutput(&want)
		flags.PrintDefaults()
		if want.Len() == 0 {
			t.Fatalf("SPEC.md section %s has no flags", number)
		}
		// -key comes first, so no run can touch the real key file.
		out, _ := exec.Command(filepath.Join(bin, cmd), "-key="+filepath.Join(t.TempDir(), "unused.key"), "-h").CombinedOutput()
		got := string(out)
		if i := strings.Index(got, "  -"); i >= 0 {
			got = got[i:] // after the "Usage of" line
		}
		if got != want.String() {
			t.Errorf("%s -h prints\n%s\nand SPEC.md section %s says\n%s", cmd, got, number, want.String())
		}
	}
}

// TestUsage runs the commands with -h, with bad flags and without the
// arguments they need, and checks the exit statuses in SPEC.md sections 6, 7
// and 9. Every run must write to stderr, and none may write to stdout or make
// a key file.
func TestUsage(t *testing.T) {
	bin := build(t, "github.com/carsonfarmer/iroh-acp-go/cmd/...")
	keyFile := filepath.Join(t.TempDir(), "usage.key")
	sk, err := key.SecretKeyFromSlice(bytes.Repeat([]byte{1}, key.SeedSize))
	if err != nil {
		t.Fatal(err)
	}
	allow := "-allow=" + sk.Public().EndpointID().String()
	for _, c := range []struct {
		cmd    string
		args   []string
		status int
	}{
		{"acp-server", []string{"-h"}, 0},
		{"acp-server", []string{"-nope", "x", allow, "sh"}, 2},
		{"acp-server", []string{"-allow"}, 2},
		{"acp-server", []string{"-allow=nope", "sh"}, 2},
		{"acp-server", []string{"sh"}, 1},
		{"acp-server", []string{allow}, 1},
		{"acp-server", nil, 1},
		{"acp-client", []string{"-h"}, 0},
		{"acp-client", []string{"-nope"}, 2},
		{"acp-client", []string{"-key"}, 2},
	} {
		// -key comes first, so no run can touch the real key file.
		args := append([]string{"-key=" + keyFile}, c.args...)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		cmd := exec.CommandContext(ctx, filepath.Join(bin, c.cmd), args...)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		cancel()
		if cmd.ProcessState == nil {
			t.Fatal(err)
		}
		if got := cmd.ProcessState.ExitCode(); got != c.status || stderr.Len() == 0 {
			t.Errorf("%s %v: status %d, stderr %q; want status %d and a message", c.cmd, args, got, stderr.String(), c.status)
		}
		if c.status == 1 && !strings.Contains(stderr.String(), "usage: acp-server") {
			t.Errorf("%s %v: stderr %q, want the usage line", c.cmd, args, stderr.String())
		}
		if stdout.Len() > 0 {
			t.Errorf("%s %v wrote %q to stdout", c.cmd, args, stdout.String())
		}
		if _, err := os.Stat(keyFile); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s %v made a key file: %v", c.cmd, args, err)
		}
	}
}

// TestSize holds the code to the budget in the README: under 100 lines for the
// library and under 50 for each command, not counting tests, blank lines and
// comments. It counts every file in a package, so moving code to another file
// does not help.
func TestSize(t *testing.T) {
	for dir, budget := range map[string]int{".": 100, "cmd/acp-server": 50, "cmd/acp-client": 50} {
		files, err := filepath.Glob(filepath.Join("..", dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			for line := range strings.Lines(string(src)) {
				if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "//") {
					n++
				}
			}
		}
		t.Logf("%s: %d lines of code", dir, n)
		if n == 0 || n >= budget {
			t.Errorf("%s has %d lines of code, want between 1 and %d", dir, n, budget-1)
		}
	}
}
