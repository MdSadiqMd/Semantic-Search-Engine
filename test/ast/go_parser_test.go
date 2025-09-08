package test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/ast"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
)

func writeTempFileGolang(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "test.go")
	err := os.WriteFile(file, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return file
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func TestGoParser_ParseFile_Function(t *testing.T) {
	parser := ast.NewGoParser()

	src := `package main
// Add adds two numbers
func Add(a int, b int) int {
	println("hi")
	return a + b
}`

	file := writeTempFileGolang(t, src)

	elements, rels, err := parser.ParseFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(elements))
	}
	fn := elements[0]

	if fn.Type != models.Function {
		t.Errorf("expected type Function, got %v", fn.Type)
	}
	if fn.Name != "Add" {
		t.Errorf("expected name Add, got %s", fn.Name)
	}

	wantSig := "func Add(a int, b int) int"
	if fn.Signature != wantSig {
		t.Errorf("unexpected function signature:\n got  %q\n want %q", fn.Signature, wantSig)
	}

	wantCode := `func Add(a int, b int) int {
	println("hi")
	return a + b
}`
	if normalizeNewlines(fn.Code) != normalizeNewlines(wantCode) {
		t.Errorf("unexpected code snippet:\n got:\n%s\n want:\n%s", fn.Code, wantCode)
	}

	if fn.DocComment != "Add adds two numbers" {
		t.Errorf("unexpected doc comment: got %q", fn.DocComment)
	}

	if fn.Metadata["param_count"] != 2 {
		t.Errorf("expected param_count=2, got %v", fn.Metadata["param_count"])
	}
	if fn.Metadata["return_count"] != 1 {
		t.Errorf("expected return_count=1, got %v", fn.Metadata["return_count"])
	}

	if len(rels) == 0 {
		t.Errorf("expected at least one relationship (function call), got 0")
	}

	if !t.Failed() {
		t.Logf("✅ TestGoParser_ParseFile_Function passed")
	}
}

func TestGoParser_ParseFile_Struct(t *testing.T) {
	parser := ast.NewGoParser()

	src := `package main
// Person represents a user
type Person struct {
	Name string
	Age  int
}`

	file := writeTempFileGolang(t, src)

	elements, rels, err := parser.ParseFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(elements))
	}

	elem := elements[0]
	if elem.Type != models.Struct {
		t.Errorf("expected type Struct, got %v", elem.Type)
	}
	if elem.Name != "Person" {
		t.Errorf("expected struct name Person, got %s", elem.Name)
	}

	wantSig := "type Person struct { ... }"
	if elem.Signature != wantSig {
		t.Errorf("unexpected signature:\n got  %q\n want %q", elem.Signature, wantSig)
	}

	wantCode := `type Person struct {
	Name string
	Age  int
}`
	if normalizeNewlines(elem.Code) != normalizeNewlines(wantCode) {
		t.Errorf("unexpected code snippet:\n got:\n%s\n want:\n%s", elem.Code, wantCode)
	}

	if len(rels) < 2 {
		t.Errorf("expected relationships for struct fields, got %d", len(rels))
	}

	if !t.Failed() {
		t.Logf("✅ TestGoParser_ParseFile_Struct passed")
	}
}

func TestGoParser_ParseFile_Import(t *testing.T) {
	parser := ast.NewGoParser()

	src := `package main
import f "fmt"`

	file := writeTempFileGolang(t, src)

	elements, _, err := parser.ParseFile(context.Background(), file)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}

	if len(elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(elements))
	}

	imp := elements[0]
	if imp.Type != models.Import {
		t.Errorf("expected type Import, got %v", imp.Type)
	}
	if imp.Name != "fmt" {
		t.Errorf("expected import name fmt, got %s", imp.Name)
	}
	if alias, ok := imp.Metadata["alias"].(string); !ok || alias != "f" {
		t.Errorf("expected alias f, got %v", imp.Metadata["alias"])
	}

	want := `import f "fmt"`
	if imp.Signature != want {
		t.Errorf("unexpected import signature:\n got  %q\n want %q", imp.Signature, want)
	}
	if imp.Code != want {
		t.Errorf("unexpected import code:\n got  %q\n want %q", imp.Code, want)
	}

	if !t.Failed() {
		t.Logf("✅ TestGoParser_ParseFile_Import passed")
	}
}
