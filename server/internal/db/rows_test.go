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
// reads rows.Err() once per loop, so a read cut short is an error and never a
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
			// Per variable: every "for x.Next()" loop needs its own x.Err(),
			// whatever the variable is called and however often it is reused.
			loops, errs := map[string][]token.Pos{}, map[string]int{}
			ast.Inspect(fd.Body, func(m ast.Node) bool {
				if fs, ok := m.(*ast.ForStmt); ok {
					if name, ok := noArgCall(fs.Cond, "Next"); ok {
						loops[name] = append(loops[name], fs.Pos())
					}
				}
				if e, ok := m.(ast.Expr); ok {
					if name, ok := noArgCall(e, "Err"); ok {
						errs[name]++
					}
				}
				return true
			})
			for name, at := range loops {
				if errs[name] < len(at) {
					t.Errorf("%s: %s has %d %s.Next() loops but reads %s.Err() %d times", fset.Position(at[0]), fd.Name.Name, len(at), name, name, errs[name])
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

// noArgCall reports the variable x when e is the call x.method().
func noArgCall(e ast.Expr, method string) (string, bool) {
	c, ok := e.(*ast.CallExpr)
	if !ok || len(c.Args) > 0 {
		return "", false
	}
	s, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != method {
		return "", false
	}
	id, ok := s.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}
