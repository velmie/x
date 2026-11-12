package pathexpr_test

import (
	"reflect"
	"testing"

	"github.com/velmie/x/pathexpr"
)

func TestInspectPathPositions(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   pathexpr.Info
	}{
		{
			name:   "single path with leading whitespace",
			source: "  event . id",
			want: pathexpr.Info{
				Paths: []string{"event.id"},
				PathPositions: []pathexpr.Position{
					{
						Offset: 2,
						Line:   1,
						Column: 3,
					},
				},
			},
		},
		{
			name:   "nested fields and CRLF",
			source: " \r\n{\r\n  名: event . id?,\r\n  nested: { event.id??, other: [0].name, again: event.id },\r\n  root: .\r\n}",
			want: pathexpr.Info{
				IsObject: true,
				Paths:    []string{"event.id", "event.id", "[0].name", "event.id", "."},
				PathPositions: []pathexpr.Position{
					{
						Offset: 13,
						Line:   3,
						Column: 6,
					},
					{
						Offset: 39,
						Line:   4,
						Column: 13,
					},
					{
						Offset: 58,
						Line:   4,
						Column: 32,
					},
					{
						Offset: 75,
						Line:   4,
						Column: 49,
					},
					{
						Offset: 96,
						Line:   5,
						Column: 9,
					},
				},
				Keys:  []string{"名", "nested", "event.id", "other", "again", "root"},
				Depth: 2,
			},
		},
		{
			name:   "literal has no paths",
			source: "true",
			want:   pathexpr.Info{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := pathexpr.Compile(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			got := expr.Inspect()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
			if len(got.Paths) > 0 {
				got.Paths[0] = "changed"
				got.PathPositions[0] = pathexpr.Position{}
				if len(got.Keys) > 0 {
					got.Keys[0] = "changed"
				}
				if again := expr.Inspect(); !reflect.DeepEqual(again, tc.want) {
					t.Fatalf("modified inspection affected subsequent result: %#v", again)
				}
			}
		})
	}
}
