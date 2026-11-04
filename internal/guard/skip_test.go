package guard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"testing"
)

var serverVariable = regexp.MustCompile(`LODE_TEST_[A-Z0-9_]+`)

// namesAServer reports whether text names a LODE_TEST_ variable other than escape_test.go's
// LODE_TEST_HOME_QUERY and LODE_TEST_HOME_FILE, which are how it talks to a process of its
// own, not servers.
func namesAServer(text string) bool {
	for _, name := range serverVariable.FindAllString(text, -1) {
		if name != "LODE_TEST_HOME_QUERY" && name != "LODE_TEST_HOME_FILE" {
			return true
		}
	}
	return false
}

// mentionsAServer reports whether a string literal under n names a server variable.
func mentionsAServer(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && namesAServer(lit.Value) {
			found = true
		}
		return !found
	})
	return found
}

func isSkip(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Skip", "Skipf", "SkipNow":
		return true
	}
	return false
}

// skipsOnAServer returns, in order, the lines of src that skip a test in a function that
// names a server variable, however the skip is reached. It sees only string literals, so a
// variable named outside the function gets past it.
func skipsOnAServer(t *testing.T, name, src string) []int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !mentionsAServer(fn.Body) {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if isSkip(n) {
				lines = append(lines, fset.Position(n.Pos()).Line)
			}
			return true
		})
	}
	return lines
}

func TestTheSkipPatternFindsADirectSkipOnAServer(t *testing.T) {
	for _, c := range []struct {
		name, src string
		skips     int
	}{
		{"a skip naming the variable", `package p
func TestX(t *testing.T) {
	if len(servers()) == 0 {
		t.Skip("set LODE_TEST_ORACLE_DSN")
	}
}`, 1},
		{"a Skipf naming the variable", `package p
func TestX(t *testing.T) { t.Skipf("no %s", "LODE_TEST_ORACLE_DSN") }`, 1},
		{"a skip under a condition on the variable", `package p
func TestX(t *testing.T) {
	if os.Getenv("LODE_TEST_ORACEL_DSN") == "" {
		t.Skip("no server")
	}
}`, 1},
		{"a SkipNow under an init on the variable", `package p
func TestX(t *testing.T) {
	if dsn := os.Getenv("LODE_TEST_ORACLE_DSN"); dsn == "" {
		t.SkipNow()
	}
}`, 1},
		{"a skip on a variable read a line before", `package p
func TestX(t *testing.T) {
	dsn := os.Getenv("LODE_TEST_ORACLE_DSN")
	if dsn == "" {
		t.Skip("no server")
	}
}`, 1},
		{"a skip in an else", `package p
func TestX(t *testing.T) {
	if dsn := os.Getenv("LODE_TEST_ORACLE_DSN"); dsn != "" {
		run(dsn)
	} else {
		t.Skip("no server")
	}
}`, 1},
		{"a variable named outside the function, which gets past it", `package p
func eachServer(t *testing.T) {
	t.Run("Oracle", func(t *testing.T) {
		if os.Getenv(oracle) == "" {
			t.Skip("no server")
		}
	})
}
var oracle = "LODE_TEST_ORACLE_DSN"`, 0},
		{"the helper", `package p
func TestX(t *testing.T) {
	if os.Getenv("LODE_TEST_ORACLE_DSN") == "" {
		adaptertest.NoServer(t, "LODE_TEST_ORACLE_DSN", "set it")
	}
}`, 0},
		{"escape_test.go's own process", `package p
func TestX(t *testing.T) {
	if query := os.Getenv("LODE_TEST_HOME_QUERY"); query == "" {
		t.Skip("not the child LODE_TEST_HOME_FILE")
	}
}`, 0},
		{"a platform skip", `package p
func TestX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits")
	}
}`, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := skipsOnAServer(t, "fixture_test.go", c.src); len(got) != c.skips {
				t.Errorf("found %d skips (lines %v), want %d", len(got), got, c.skips)
			}
		})
	}
}

func TestServerTestsSkipOnlyThroughTheHelper(t *testing.T) {
	for _, rel := range files(t, isTestFile) {
		for _, line := range skipsOnAServer(t, rel, string(read(t, rel))) {
			t.Errorf("%s:%d skips in a function that names a LODE_TEST_ server variable; call adaptertest.NoServer instead, so the full suite fails when the server is missing, and keep any other skip out of that function", rel, line)
		}
	}
}
