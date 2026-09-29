package regenerate

import (
	"bytes"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

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
// section of SPEC.md: the names, the defaults and the usage text. It prints
// the table's flags with the flag package and expects the same lines.
func TestFlags(t *testing.T) {
	bin := build(t, "github.com/carsonfarmer/iroh-acp-go/cmd/...")
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("(?m)^\\| `-([a-z]+)` \\| (.+?) \\| .*Usage text: ``(.+?)``\\. \\|$")
	for number, cmd := range map[string]string{"6": "acp-server", "7": "acp-client"} {
		flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
		for _, m := range row.FindAllStringSubmatch(specSection(t, number), -1) {
			def := strings.Trim(m[2], "`")
			switch {
			case def == m[2]: // not code, as in "none, required"
				def = ""
			case strings.HasPrefix(def, "<os.UserConfigDir()>/"):
				def = filepath.Join(configDir, strings.TrimPrefix(def, "<os.UserConfigDir()>/"))
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
