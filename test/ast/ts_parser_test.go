package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/ast"
	"github.com/MdSadiqMd/Semantic-Search-Engine/internal/models"
	"github.com/stretchr/testify/assert"
)

func writeTempFileTypescript(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}

func TestTypeScriptParser_ParseFile_Imports(t *testing.T) {
	parser := ast.NewTypeScriptParser()
	dir := t.TempDir()

	src := `
		import fs from "fs";
		const http = require("http");
	`
	file := writeTempFileTypescript(t, dir, "imports.ts", src)

	elements, rels, err := parser.ParseFile(context.Background(), file)
	assert.NoError(t, err)
	assert.Empty(t, rels)

	var imports []models.CodeElement
	for _, e := range elements {
		if e.Type == models.Import {
			imports = append(imports, e)
		}
	}

	assert.Len(t, imports, 2)
	names := map[string]bool{}
	for _, imp := range imports {
		names[imp.Name] = true
	}

	assert.Contains(t, names, "fs")
	assert.Contains(t, names, "http")
}

func TestTypeScriptParser_ParseFile_Functions(t *testing.T) {
	parser := ast.NewTypeScriptParser()
	dir := t.TempDir()

	src := `
		// adds numbers
		export async function add(a: number, b: number): number {
			return a + b;
		}
	`
	file := writeTempFileTypescript(t, dir, "func.ts", src)

	elements, rels, err := parser.ParseFile(context.Background(), file)
	assert.NoError(t, err)

	var funcs []models.CodeElement
	for _, e := range elements {
		if e.Type == models.Function {
			funcs = append(funcs, e)
		}
	}
	assert.Len(t, funcs, 1)
	assert.Equal(t, "add", funcs[0].Name)
	assert.Equal(t, "export async function add(a: number, b: number): number {", funcs[0].Signature)
	assert.Contains(t, funcs[0].DocComment, "adds numbers")

	if assert.NotEmpty(t, rels) {
		assert.Contains(t, rels[0].Properties["function_name"], "return")
	}
}

func TestTypeScriptParser_ParseFile_Class(t *testing.T) {
	parser := ast.NewTypeScriptParser()
	dir := t.TempDir()

	src := `
		export class Person {
			private name: string;
			constructor(name: string) {
				this.name = name;
			}
			greet(): string {
				return "Hello " + this.name;
			}
		}
	`
	file := writeTempFileTypescript(t, dir, "class.ts", src)

	elements, rels, err := parser.ParseFile(context.Background(), file)
	assert.NoError(t, err)

	var classes, methods, vars []models.CodeElement
	for _, e := range elements {
		switch e.Type {
		case models.Struct:
			classes = append(classes, e)
		case models.Method:
			methods = append(methods, e)
		case models.Variable:
			vars = append(vars, e)
		}
	}
	assert.Len(t, classes, 1)
	assert.Equal(t, "Person", classes[0].Name)

	assert.Len(t, methods, 1)
	assert.Equal(t, "greet", methods[0].Name)

	assert.Len(t, vars, 1)
	assert.Equal(t, "name", vars[0].Name)

	assert.NotEmpty(t, rels)
}

func TestTypeScriptParser_ParseFile_InterfaceAndTypeAndVar(t *testing.T) {
	parser := ast.NewTypeScriptParser()
	dir := t.TempDir()

	src := `
		export interface Shape {
			area(): number;
		}
		export type ID = string | number;
		export const PI = 3.14;
		let counter = 0;
	`
	file := writeTempFileTypescript(t, dir, "misc.ts", src)

	elements, _, err := parser.ParseFile(context.Background(), file)
	assert.NoError(t, err)

	var ifaces, types, consts, vars []models.CodeElement
	for _, e := range elements {
		switch e.Type {
		case models.Interface:
			ifaces = append(ifaces, e)
		case models.Struct:
			types = append(types, e)
		case models.Constant:
			consts = append(consts, e)
		case models.Variable:
			vars = append(vars, e)
		}
	}

	assert.Len(t, ifaces, 1)
	assert.Equal(t, "Shape", ifaces[0].Name)

	assert.Len(t, types, 1)
	assert.Equal(t, "ID", types[0].Name)

	assert.Len(t, consts, 1)
	assert.Equal(t, "PI", consts[0].Name)

	assert.Len(t, vars, 1)
	assert.Equal(t, "counter", vars[0].Name)
}
