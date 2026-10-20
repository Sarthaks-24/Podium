package integrity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ratingTable matches a query that reads or writes a rating table (a
// column named after one, like notifications.rating_history_id, is not).
var ratingTable = regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE)\s+(student_rating|rating_history|rating_proposals|rating_proposal_closures|rating_rollovers)\b`)

// The rating is shown, never used (D110): standings, exports and
// summaries (package contests), notices (package notifications) and
// progress (httpapi/progress.go) never import integrity, never name
// anything declared in rating.go, and never query the rating tables.
func TestNothingReadsTheRatingButItsPages(t *testing.T) {
	fset := token.NewFileSet()
	rating, err := parser.ParseFile(fset, "rating.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, d := range rating.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			declared[d.Name.Name] = true // functions and methods (Rating, RatingOf, StandingOf...)
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				switch sp := sp.(type) {
				case *ast.TypeSpec:
					declared[sp.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range sp.Names {
						declared[n.Name] = true
					}
				}
			}
		}
	}
	if !declared["BandOf"] || !declared["RatingOf"] || !declared["applyRatingTx"] {
		t.Fatalf("rating.go declarations not read: %v", declared)
	}
	var files []string
	for _, pattern := range []string{"../contests/*.go", "../notifications/*.go"} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	files = append(files, "../httpapi/progress.go")
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, im := range file.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); strings.HasSuffix(p, "/internal/integrity") {
				t.Errorf("%s imports integrity", f)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && x.Name == "integrity" && declared[n.Sel.Name] {
					t.Errorf("%s uses integrity.%s from rating.go", f, n.Sel.Name)
				}
				if declared[n.Sel.Name] && (n.Sel.Name == "Rating" || n.Sel.Name == "RatingOf" || n.Sel.Name == "Ratings" ||
					n.Sel.Name == "StandingOf" || n.Sel.Name == "EditRating") {
					t.Errorf("%s calls %s", f, n.Sel.Name)
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING && ratingTable.MatchString(n.Value) {
					t.Errorf("%s queries a rating table: %s", f, n.Value)
				}
			}
			return true
		})
	}
	if checked < 10 {
		t.Fatalf("only %d files checked", checked)
	}
}
