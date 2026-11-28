package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// CLI の出力・エラーの文言は英語で、ASCII のみとする（CLAUDE.md「Go」。Windows の OEM の
// コードページのコンソールで文字化けしないため）。テスト以外のコードの文字列リテラルを、cli/ の下の
// 全てで検証する（コメントは日本語でよい）。
func TestStringLiteralsAreASCII(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				for _, r := range lit.Value {
					if r >= utf8.RuneSelf {
						t.Errorf("%s: a string literal has %q", fset.Position(lit.Pos()), r)
						break
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
