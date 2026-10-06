// Package ssa предоставляет функции для построения SSA представления
package ssa

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Builder отвечает за построение SSA из исходного кода Go
type Builder struct {
	fset *token.FileSet
}

// NewBuilder создаёт новый экземпляр Builder
func NewBuilder() *Builder {
	return &Builder{
		fset: token.NewFileSet(),
	}
}

// ParseAndBuildSSA парсит исходный код Go и создаёт SSA представление
// Возвращает SSA программу и функцию по имени
func (b *Builder) ParseAndBuildSSA(source string, funcName string) (*ssa.Function, error) {
	file, err := parser.ParseFile(b.fset, "input.go", source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("не удалось разобрать исходный код: %w", err)
	}

	info := &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Instances:    make(map[*ast.Ident]types.Instance),
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Implicits:    make(map[ast.Node]types.Object),
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:       make(map[ast.Node]*types.Scope),
		FileVersions: make(map[*ast.File]string),
	}
	pkg := types.NewPackage("analysis", file.Name.Name)
	if err := types.NewChecker(&types.Config{}, b.fset, pkg, info).Files([]*ast.File{file}); err != nil {
		return nil, fmt.Errorf("не удалось построить SSA: %w", err)
	}

	program := ssa.NewProgram(b.fset, ssa.SanityCheckFunctions)
	ssaPkg := program.CreatePackage(pkg, []*ast.File{file}, info, false)
	ssaPkg.Build()

	function := ssaPkg.Func(funcName)
	if function == nil {
		return nil, fmt.Errorf("функция %q не найдена", funcName)
	}

	return function, nil
}
