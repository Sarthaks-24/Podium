package integrity

import (
	"math/rand"
	"strings"
	"testing"
)

func texts(toks []Token) string {
	parts := make([]string, len(toks))
	for i, t := range toks {
		parts[i] = t.Text
	}
	return strings.Join(parts, " ")
}

func TestLexNormalises(t *testing.T) {
	cases := []struct{ lang, src, want string }{
		{"cpp17", "#include <bits/stdc++.h>\n#define F(i) \\\n  for(;;)\nint main(){ long long total = 42; // sum\n return total; }",
			"int I ( ) { long long I = N ; return I ; }"},
		{"cpp17", `auto s = R"x(a "quoted" )" here)x"; char c = '\''; s += u8"y";`,
			"auto I = S ; char I = S ; I += S ;"},
		{"cpp17", "int x = 1'000'000 + 0x1Fu + 1.5e-3;", "int I = N + N + N ;"},
		{"java21", "String t = \"\"\"\n  block \"\"\";\n/* c */ int a = b >>> 2;", "I I = S ; int I = I >>> N ;"},
		{"python3", "def f(n):\n    s = r'\\d' + f\"{n}\" + '''x\n'''  # note\n    return n // 2", "def I ( I ) : I = S + S + S return I // N"},
		{"node22", "const s = `a ${ {b: 1}.b } c`; let x = y ?? z?.w; // end", "const I = S ; let I = I ?? I ?. I ;"},
		{"python3", "nombre = 'unterminated\nx = 1", "I = S I = N"},
	}
	for _, c := range cases {
		if got := texts(Lex(c.lang, c.src)); got != c.want {
			t.Errorf("%s %q:\n got  %s\n want %s", c.lang, c.src, got, c.want)
		}
	}
}

func TestLexOffsetsCoverTheSource(t *testing.T) {
	src := "int  main() {\n  return 0;\n}"
	for _, tok := range Lex("cpp17", src) {
		if tok.Start < 0 || tok.End > len(src) || tok.Start >= tok.End {
			t.Fatalf("bad span %+v", tok)
		}
	}
	toks := Lex("cpp17", src)
	if src[toks[1].Start:toks[1].End] != "main" {
		t.Errorf("second token covers %q", src[toks[1].Start:toks[1].End])
	}
}

// Renaming identifiers, changing literals, comments and layout leaves the
// token stream unchanged.
func TestLexIgnoresDisguises(t *testing.T) {
	a := "int sum(int a, int b) { return a + b; } // add\nint main() { int x = 3; }"
	b := "/* helper */ int   add_two( int p,int q ){return p+q;}\n\nint main(){int value=7;}"
	if texts(Lex("cpp17", a)) != texts(Lex("cpp17", b)) {
		t.Errorf("disguise changed tokens:\n%s\n%s", texts(Lex("cpp17", a)), texts(Lex("cpp17", b)))
	}
}

func TestLexNeverPanicsOnJunk(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	alphabet := []byte("\"'`\\/*#${}()R\nx0.e+-<>=!?\xff\xfe")
	for _, lang := range []string{"cpp17", "java21", "python3", "node22", "other"} {
		for i := 0; i < 2000; i++ {
			b := make([]byte, r.Intn(64))
			for j := range b {
				b[j] = alphabet[r.Intn(len(alphabet))]
			}
			for _, tok := range Lex(lang, string(b)) {
				if tok.Start >= tok.End || tok.End > len(b) {
					t.Fatalf("%s %q: bad span %+v", lang, b, tok)
				}
			}
		}
	}
}

// Any shared run of W+K-1 tokens yields a shared fingerprint, whatever
// surrounds it.
func TestWinnowingGuarantee(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	words := []string{"if", "(", ")", "{", "}", "I", "N", "+", "=", ";", "for", "return", "while", "<"}
	rnd := func(n int) []Token {
		out := make([]Token, n)
		for i := range out {
			out[i] = Token{Text: words[r.Intn(len(words))], Start: i, End: i + 1}
		}
		return out
	}
	for trial := 0; trial < 500; trial++ {
		common := rnd(W + K - 1)
		a := append(append(rnd(r.Intn(30)), common...), rnd(r.Intn(30))...)
		b := append(append(rnd(r.Intn(30)), common...), rnd(r.Intn(30))...)
		set := map[uint64]bool{}
		for _, f := range Winnow(a) {
			set[f.Hash] = true
		}
		found := false
		for _, f := range Winnow(b) {
			found = found || set[f.Hash]
		}
		if !found {
			t.Fatalf("trial %d: a shared run of %d tokens left no shared fingerprint", trial, W+K-1)
		}
	}
}
