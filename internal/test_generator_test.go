package internal

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateAndRun(t *testing.T) {
	dir := t.TempDir()
	source := `package fixture
type Number int16
func named(x Number) Number { return x+1 }
func branch(x int8, unused bool) int8 { if x > 0 { return x + 127 }; return -x }
func unsigned(x uint64) uint64 { if x > 9223372036854775807 { return x }; return 18446744073709551615 }
func boolean(a,b bool) bool { return a && b }
func pair(x int) (int,bool) { return x+1, x<0 }
func nothing() {}
func constant() int { return 42 }
func divide(x,y int) int { return x/y }
func explicit(x bool) { if x { panic("expected") } }
func nilPanic() { panic(nil) }
func floating(a,b float64) float64 { c:=a+1.1; if b+c>10.1 && b+c<11.125 { return 1.1 }; return 1.2 }
func floatDivide(a,b float64) float64 { return a / b }
func floatNegate(a float64) float64 { return -a }
func negativeZero() float64 { return -1.0 / 1e300 / 1e300 }
`
	path := filepath.Join(dir, "fixture.go")
	mustWrite(t, path, source)
	mustWrite(t, filepath.Join(dir, "go.mod"), "module fixture\n\ngo 1.26.2\n")
	t.Chdir(dir)
	generated := GenerateTestFile("fixture.go")
	if generated != filepath.Join(dir, "fixture_test.go") {
		t.Fatal(generated)
	}
	content, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"named", "floatDivide", "floatNegate", "branch", "unsigned", "boolean", "pair", "nothing", "constant", "divide", "explicit", "nilPanic", "floating", "negativeZero"} {
		if !strings.Contains(string(content), "func TestGenerated_"+name+"(") {
			t.Errorf("missing %s", name)
		}
	}
	expectPanic(t, "input must not be a test file", func() { GenerateTestFile(generated) })
	GenerateTestFile(path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated tests: %v\n%s", err, output)
	}
	mustWrite(t, path, strings.Replace(source, "return 42", "return 43", 1))
	broken := exec.CommandContext(ctx, "go", "test", "-count=1", "./...")
	broken.Dir = dir
	if output, err := broken.CombinedOutput(); err == nil || !strings.Contains(string(output), "TestGenerated_constant") {
		t.Fatalf("generated assertions failed to detect changed result: %v\n%s", err, output)
	}
}

func TestGenerateErrors(t *testing.T) {
	for _, tt := range []struct{ name, source, existing, want string }{
		{"syntax", "package", "", "generate tests"},
		{"unsupported", "package fixture\nfunc f(s string) string { return s }", "", "f:"},
		{"manual", "package fixture\nfunc f() {}", "package fixture\n", "manual"},
		{"method", "package fixture\ntype S int\nfunc(s S) f() {}", "", "methods"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "fixture.go")
			mustWrite(t, path, tt.source)
			target := filepath.Join(dir, "fixture_test.go")
			if tt.existing != "" {
				mustWrite(t, target, tt.existing)
			}
			expectPanic(t, tt.want, func() { GenerateTestFile(path) })
			if tt.existing != "" {
				data, _ := os.ReadFile(target)
				if string(data) != tt.existing {
					t.Fatal("overwrote manual file")
				}
			} else if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatal("wrote partial output")
			}
		})
	}
	expectPanic(t, "generate tests", func() { GenerateTestFile(filepath.Join(t.TempDir(), "missing.go")) })
}

func TestGenerateLimits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.go")
	mustWrite(t, path, "package fixture\nfunc f(x bool) int { if x { for {} }; return 1 }")
	target := GenerateTestFile(path)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Skipped 1 paths") {
		t.Fatalf("missing limit diagnostic: %s", data)
	}
	mustWrite(t, path, "package fixture\nfunc f() { for {} }")
	expectPanic(t, "no completed", func() { GenerateTestFile(path) })
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}
func expectPanic(t *testing.T, want string, action func()) {
	t.Helper()
	defer func() {
		failure := recover()
		if failure == nil || !strings.Contains(fmt.Sprint(failure), want) {
			t.Errorf("expected panic containing %q, got %v", want, failure)
		}
	}()
	action()
}
