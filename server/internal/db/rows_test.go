package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRowsAreChecked reads the server's production Go files (S1-08): no
// Scan result is thrown away, and every function that loops on rows.Next()
// also reads rows.Err(), so a read cut short is an error and never a
// silently shorter list or a zero score.
func TestRowsAreChecked(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if es, ok := n.(*ast.ExprStmt); ok {
				if c, ok := es.X.(*ast.CallExpr); ok {
					if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Scan" && len(c.Args) > 0 {
						t.Errorf("%s: the error of Scan is thrown away", fset.Position(es.Pos()))
					}
				}
			}
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				return true
			}
			next, checked := map[string]token.Pos{}, map[string]bool{}
			ast.Inspect(fd.Body, func(m ast.Node) bool {
				c, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				s, ok := c.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if id, ok := s.X.(*ast.Ident); ok && id.Name == "rows" {
					switch s.Sel.Name {
					case "Next":
						next[id.Name] = c.Pos()
					case "Err":
						checked[id.Name] = true
					}
				}
				return true
			})
			for name, pos := range next {
				if !checked[name] {
					t.Errorf("%s: %s loops on %s.Next() without reading %s.Err()", fset.Position(pos), fd.Name.Name, name, name)
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
