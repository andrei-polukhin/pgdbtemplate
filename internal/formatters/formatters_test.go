package formatters_test

import (
	"testing"

	qt "github.com/frankban/quicktest"

	"github.com/andrei-polukhin/pgdbtemplate/internal/formatters"
)

func TestQuoteIdentifier(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	var cases = []struct {
		input string
		want  string
	}{
		{`foo`, `"foo"`},
		{`foo bar baz`, `"foo bar baz"`},
		{`foo"bar`, `"foo""bar"`},
		{"foo\x00bar", `"foo"`},
		{"\x00foo", `""`},
	}

	for _, test := range cases {
		got := formatters.QuoteIdentifier(test.input)
		c.Assert(got, qt.Equals, test.want)
	}
}

func TestQuoteLiteral(t *testing.T) {
	t.Parallel()
	c := qt.New(t)

	var cases = []struct {
		input string
		want  string
	}{
		{`foo`, `'foo'`},
		{`foo bar baz`, `'foo bar baz'`},
		{`foo'bar`, `'foo''bar'`},
		{`foo\bar`, ` E'foo\\bar'`},
		{`foo\ba'r`, ` E'foo\\ba''r'`},
		{`foo"bar`, `'foo"bar'`},
		{`foo\x00bar`, ` E'foo\\x00bar'`},
		{`\x00foo`, ` E'\\x00foo'`},
		{`'`, `''''`},
		{`''`, `''''''`},
		{`\`, ` E'\\'`},
		{`'abc'; DROP TABLE users;`, `'''abc''; DROP TABLE users;'`},
		{`\'`, ` E'\\'''`},
		{`E'\''`, ` E'E''\\'''''`},
		{`e'\''`, ` E'e''\\'''''`},
		{`E'\'abc\'; DROP TABLE users;'`, ` E'E''\\''abc\\''; DROP TABLE users;'''`},
		{`e'\'abc\'; DROP TABLE users;'`, ` E'e''\\''abc\\''; DROP TABLE users;'''`},
	}

	for _, test := range cases {
		got := formatters.QuoteLiteral(test.input)
		c.Assert(got, qt.Equals, test.want)
	}
}
