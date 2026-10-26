package ssa

import (
	"fmt"

	toolssa "golang.org/x/tools/go/ssa"
)

// PrintFunction выводит базовые блоки SSA-функции, инструкции и связи CFG.
func PrintFunction(function *toolssa.Function) {
	for _, block := range function.Blocks {
		fmt.Printf("\nБлок %d (%s)\n", block.Index, block.Comment)
		fmt.Printf("  Предшественники: %v\n", blockIndexes(block.Preds))
		fmt.Printf("  Последователи: %v\n", blockIndexes(block.Succs))
		fmt.Println("  Инструкции:")
		for _, instruction := range block.Instrs {
			fmt.Printf("    %T: %s\n", instruction, instruction)
		}
	}
}

func blockIndexes(blocks []*toolssa.BasicBlock) []int {
	indexes := make([]int, len(blocks))
	for i, block := range blocks {
		indexes[i] = block.Index
	}
	return indexes
}
