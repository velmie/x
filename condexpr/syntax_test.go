package condexpr_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/velmie/x/condexpr"
	"github.com/velmie/x/pathexpr"
)

func TestCompileRejectsInvalidConditions(t *testing.T) {
	for _, source := range []string{
		"", " ", "event.paid", "true", "false", "null", "42", "'text'", "len(event.items)",
		"!event.paid", "(event.paid)", "a == contains(b, c)", "contains(a, b) == true", "len(contains(a,b)) == 1",
		"a ==", "== a", "a = 1", "a !== 1", "a === 1", "a <== 1", "a <> 1", "a == 1 & b == 2", "a == 1 | b == 2",
		"a == 1 &&", "a == 1 ||", "a == 1 b == 2", "a == 1 == 1", "a == 1)", "(a == 1", "()",
		"contains(a)", "contains(a,b,c)", "contains(a,b,)", "contains(,b)", "len() == 1", "len(a,b) == 1", "unknown(a,b)",
		"event. == 1", "event..a == 1", "a[01] == 1", "a[-1] == 1", "a[] == 1", "a[0 == 1", "a[0]b == 1",
		"a[18446744073709551616] == 1", "a?? == null", "1? == 1", "true? == true", "null? == null", "'a'? == 'a'",
		"a == [1,2]", "a == {key:1}", "a + 1 == 2", "a in b", "a == \"b\"", "'bad\\q' == ''", "'unclosed", "'unfinished\\",
		"a b == 1", "a\\b == 1", "\xff == 1", "a == '\xff'", "a == 1\xff",
	} {
		t.Run(source, func(t *testing.T) {
			_, err := condexpr.Compile(source)
			var syntax *condexpr.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("expected SyntaxError, got %v", err)
			}
		})
	}
}

func TestSyntaxPositions(t *testing.T) {
	for _, tc := range []struct {
		source string
		offset int
		line   int
		column int
	}{
		{
			source: "a == 1 &&\r\n  true",
			offset: 17,
			line:   2,
			column: 7,
		},
		{
			source: "名 == 1 &&\r\n  a[01] == 2",
			offset: 19,
			line:   2,
			column: 7,
		},
		{
			source: "'a\\é' == ''",
			offset: 3,
			line:   1,
			column: 4,
		},
		{
			source: "a == 1 &&\r  b.\r\n == 2",
			offset: 17,
			line:   3,
			column: 2,
		},
		{
			source: "a == 1\n&& \xff",
			offset: 10,
			line:   2,
			column: 4,
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, err := condexpr.Compile(tc.source)
			var syntax *condexpr.SyntaxError
			if !errors.As(err, &syntax) {
				t.Fatalf("expected SyntaxError, got %v", err)
			}
			if syntax.Pos.Offset != tc.offset || syntax.Pos.Line != tc.line || syntax.Pos.Column != tc.column {
				t.Fatalf("position = %+v, want offset %d line %d column %d: %v", syntax.Pos, tc.offset, tc.line, tc.column, err)
			}
			if errors.Unwrap(syntax) == nil {
				t.Fatal("missing syntax cause")
			}
		})
	}
}

func TestEvaluationDiagnostics(t *testing.T) {
	for _, optional := range []string{"", "?"} {
		source := "名 == 1 &&\r\n  event.\n    secret" + optional + " == 1"
		expr, err := condexpr.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(map[string]any{"名": 1, "event": "private-input-value"})
		var detail *condexpr.EvalError
		var path *pathexpr.EvalError
		var typed *pathexpr.TypeError
		if got || !errors.As(err, &detail) || !errors.As(err, &path) || !errors.As(err, &typed) {
			t.Fatalf("got %v, %v", got, err)
		}
		if path.Path != "event.secret" || path.At != "event" || path.Pos.Line != 3 || path.Pos.Column != 5 || path.Pos.Offset != strings.Index(source, "secret") || detail.Pos != path.Pos {
			t.Fatalf("location: %+v / %+v", path, detail)
		}
		if err.Error() != path.Error() {
			t.Fatalf("path failure repeated context: %v", err)
		}
		if typed.Want != "string-keyed map" || typed.Got != reflect.TypeOf("") || !strings.Contains(err.Error(), "event.secret") || strings.Contains(err.Error(), "private-input-value") {
			t.Fatalf("lost cause or leaked value: %v", err)
		}
	}
	expr, err := condexpr.Compile("a == 1 &&\n  event.total > message.minimum")
	if err != nil {
		t.Fatal(err)
	}
	got, err := expr.Eval(map[string]any{"a": 1, "event": map[string]any{"total": "private-input-value"}, "message": map[string]any{"minimum": 2}})
	var detail *condexpr.EvalError
	if got || !errors.As(err, &detail) {
		t.Fatalf("got %v, %v", got, err)
	}
	if detail.Op != ">" || detail.Pos.Line != 2 || detail.Pos.Column != 15 || !reflect.DeepEqual(detail.Paths, []string{"event.total", "message.minimum"}) {
		t.Fatalf("bad operation location: %+v", detail)
	}
	if !strings.Contains(err.Error(), "string and number") || strings.Contains(err.Error(), "private-input-value") || errors.Unwrap(detail) == nil {
		t.Fatalf("bad cause: %v", err)
	}
}

func TestNestingBoundAndLongChains(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		body   string
		suffix string
		tail   string
	}{
		{
			prefix: "(",
			body:   "1 == 1",
			suffix: ")",
		},
		{
			prefix: "!",
			body:   "1 == 1",
		},
		{
			prefix: "len(",
			body:   "null",
			suffix: ")",
			tail:   " == 0",
		},
	} {
		for _, depth := range []int{128, 129, 10000} {
			source := strings.Repeat(tc.prefix, depth) + tc.body + strings.Repeat(tc.suffix, depth) + tc.tail
			_, err := condexpr.Compile(source)
			if (err == nil) != (depth == 128) {
				t.Fatalf("prefix %s depth %d: %v", tc.prefix, depth, err)
			}
		}
	}
	for _, op := range []string{" && ", " || "} {
		source := strings.Repeat("1 == 1"+op, 10000) + "1 == 1"
		expr, err := condexpr.Compile(source)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expr.Eval(nil)
		if !got || err != nil {
			t.Fatalf("long chain: got %v, %v", got, err)
		}
	}
}

func TestEvaluationMessages(t *testing.T) {
	root := map[string]any{
		"event": map[string]any{
			"totalMinor": 100,
			"tags":       []string{"private-input-value"},
			"number":     json.Number("private-input-value"),
		},
	}
	for _, tc := range []struct {
		source string
		want   string
	}{
		{
			source: "event.total > 0",
			want:   `get path "event.total" at "event.total" (line 1, column 7): value not found`,
		},
		{
			source: "event.totalMinor > 'private-input-value'",
			want:   "operator > (line 1, column 18): unsupported operands number and string (paths: event.totalMinor)",
		},
		{
			source: "event.tags == event.tags",
			want:   "operator == (line 1, column 12): unsupported operands array and array (paths: event.tags)",
		},
		{
			source: "1 > 'private-input-value'",
			want:   "operator > (line 1, column 3): unsupported operands number and string",
		},
		{
			source: "len(true) == 1",
			want:   "function len (line 1, column 1): expected string, array or null, got boolean",
		},
		{
			source: "contains(event.totalMinor, 'private-input-value')",
			want:   "function contains (line 1, column 1): expected two strings or an array and a value, got number and string (paths: event.totalMinor)",
		},
		{
			source: "event.number == 1",
			want:   "path value (line 1, column 1): expected a finite JSON number (paths: event.number)",
		},
	} {
		t.Run(tc.source, func(t *testing.T) {
			expr, err := condexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expr.Eval(root)
			if got || err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, %v; want %s", got, err, tc.want)
			}
			var detail *condexpr.EvalError
			if !errors.As(err, &detail) || detail.Op == "literal" {
				t.Fatalf("unexpected evaluation error: %v", err)
			}
			if tc.source == "event.tags == event.tags" && !reflect.DeepEqual(detail.Paths, []string{"event.tags", "event.tags"}) {
				t.Fatalf("Paths lost occurrences: %v", detail.Paths)
			}
			if tc.source == "event.total > 0" && !errors.Is(err, pathexpr.ErrNotFound) {
				t.Fatalf("missing cause: %v", err)
			}
		})
	}
}
