package core

func heavyTasks() []codingTask {
	return []codingTask{
		{name: "diff", files: map[string]string{"diff.go": heavyDiff, "diff_test.go": heavyDiffTest}},
		{name: "glob", files: map[string]string{"glob.go": heavyGlob, "glob_test.go": heavyGlobTest}},
		{name: "sched", files: map[string]string{"sched.go": heavySched, "sched_test.go": heavySchedTest}},
		{name: "query", files: map[string]string{"query.go": heavyQuery, "query_test.go": heavyQueryTest}},
		{name: "width", files: map[string]string{"width.go": heavyWidth, "width_test.go": heavyWidthTest}},
		{name: "store", files: map[string]string{"store.go": heavyStore, "store_test.go": heavyStoreTest}},
	}
}

const heavyDiff = `package p

// Edit is one line of a minimal line diff. Op is '=', '-', or '+'.
type Edit struct {
	Op   byte
	Line string
}

// Diff returns the edit script that turns a into b. Equal lines are kept.
// A replaced line is a deletion followed by an insertion. Leftover lines on
// either side are deletions or insertions.
func Diff(a, b []string) []Edit {
	var out []Edit
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			out = append(out, Edit{Op: '=', Line: a[i]})
			i++
			j++
			continue
		}
		// A mismatch drops the old line and records only the new one.
		out = append(out, Edit{Op: '+', Line: b[j]})
		i++
		j++
	}
	for ; j < len(b); j++ {
		out = append(out, Edit{Op: '+', Line: b[j]})
	}
	for ; i < len(a); i++ {
		out = append(out, Edit{Op: '-', Line: a[i]})
	}
	return out
}
`

const heavyDiffTest = `package p

import (
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	cases := []struct {
		a, b []string
		want []Edit
	}{
		{nil, nil, nil},
		{[]string{"a", "b"}, []string{"a", "b"}, []Edit{{'=', "a"}, {'=', "b"}}},
		{[]string{"a"}, []string{"a", "b"}, []Edit{{'=', "a"}, {'+', "b"}}},
		{[]string{"a", "b"}, []string{"a"}, []Edit{{'=', "a"}, {'-', "b"}}},
		{[]string{"a", "b"}, []string{"a", "c"}, []Edit{{'=', "a"}, {'-', "b"}, {'+', "c"}}},
		{[]string{"a", "b"}, []string{"b"}, []Edit{{'-', "a"}, {'=', "b"}}},
		{[]string{"a", "b", "c"}, []string{"a", "x", "c"}, []Edit{{'=', "a"}, {'-', "b"}, {'+', "x"}, {'=', "c"}}},
	}
	for _, c := range cases {
		got := Diff(c.a, c.b)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("Diff(%#v, %#v) = %#v, want %#v", c.a, c.b, got, c.want)
		}
	}
}
`

const heavyGlob = `package p

import "strings"

// Match reports whether s matches pat. pat may contain '*' (any sequence,
// including empty) and '?' (one byte). A '*' must backtrack when a later
// literal would otherwise fail.
func Match(pat, s string) bool {
	for len(pat) > 0 {
		if pat[0] == '*' {
			pat = pat[1:]
			if pat == "" {
				return true
			}
			n := 0
			for n < len(pat) && pat[n] != '*' && pat[n] != '?' {
				n++
			}
			lit := pat[:n]
			j := strings.Index(s, lit)
			if j < 0 {
				return false
			}
			s = s[j+len(lit):]
			pat = pat[n:]
			continue
		}
		if s == "" {
			return false
		}
		if pat[0] != '?' && pat[0] != s[0] {
			return false
		}
		pat = pat[1:]
		s = s[1:]
	}
	return s == ""
}
`

const heavyGlobTest = `package p

import "testing"

func TestMatch(t *testing.T) {
	ok := [][2]string{
		{"abc", "abc"},
		{"*", ""},
		{"*", "abc"},
		{"a*", "abc"},
		{"*c", "abc"},
		{"a*c", "abc"},
		{"a*c", "ac"},
		{"?", "z"},
		{"a?c", "abc"},
		{"a*b", "ab"},
		{"a*b", "abxb"},
		{"a*b*b", "abb"},
		{"*a*", "ba"},
	}
	bad := [][2]string{
		{"abc", "ab"},
		{"a?c", "ac"},
		{"?", "ab"},
		{"a*b*b", "ab"},
		{"a*b", "a"},
		{"x*", "abc"},
	}
	for _, c := range ok {
		if !Match(c[0], c[1]) {
			t.Fatalf("Match(%q, %q) = false", c[0], c[1])
		}
	}
	for _, c := range bad {
		if Match(c[0], c[1]) {
			t.Fatalf("Match(%q, %q) = true", c[0], c[1])
		}
	}
}
`

const heavySched = `package p

import (
	"strconv"
	"strings"
	"time"
)

// Next returns the earliest UTC time at or after t whose minute and hour
// match spec. spec is "M H": each field is "*" or an integer. The result
// has zero seconds and nanoseconds. A time that already matches, including
// its seconds, is returned unchanged only when its seconds are already zero.
func Next(t time.Time, spec string) time.Time {
	t = t.UTC()
	fields := strings.Fields(spec)
	minAny, minV := clockField(fields[0])
	hourAny, hourV := clockField(fields[1])
	// Searching from the truncated minute treats 14:30:45 as 14:30:00.
	cur := t.Truncate(time.Minute)
	for step := 0; step < 8*24*60; step++ {
		minOK := minAny || cur.Minute() == minV
		hourOK := hourAny || cur.Hour() == hourV
		if minOK && hourOK {
			return cur
		}
		cur = cur.Add(time.Minute)
	}
	return time.Time{}
}

func clockField(s string) (any bool, v int) {
	if s == "*" {
		return true, 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return true, 0
	}
	return false, n
}
`

const heavySchedTest = `package p

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	utc := func(y int, m time.Month, d, hh, mm, ss int) time.Time {
		return time.Date(y, m, d, hh, mm, ss, 0, time.UTC)
	}
	cases := []struct {
		t    time.Time
		spec string
		want time.Time
	}{
		{utc(2026, 1, 2, 14, 30, 0), "30 14", utc(2026, 1, 2, 14, 30, 0)},
		{utc(2026, 1, 2, 14, 30, 45), "30 14", utc(2026, 1, 3, 14, 30, 0)},
		{utc(2026, 1, 2, 10, 15, 0), "0 *", utc(2026, 1, 2, 11, 0, 0)},
		{utc(2026, 1, 2, 10, 30, 45), "* *", utc(2026, 1, 2, 10, 31, 0)},
		{utc(2026, 1, 2, 9, 0, 0), "0 9", utc(2026, 1, 2, 9, 0, 0)},
		{utc(2026, 1, 2, 9, 0, 1), "0 9", utc(2026, 1, 3, 9, 0, 0)},
		{utc(2026, 1, 2, 23, 59, 0), "0 0", utc(2026, 1, 3, 0, 0, 0)},
	}
	for _, c := range cases {
		got := Next(c.t, c.spec)
		if !got.Equal(c.want) {
			t.Fatalf("Next(%s, %q) = %s, want %s", c.t.Format(time.RFC3339), c.spec, got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
}
`

const heavyQuery = `package p

import (
	"strconv"
	"strings"
)

// Select returns the rows for which expr is true. expr is space-separated:
// comparisons (field = value, field > n, field < n), combined with && and ||.
// && binds tighter than ||. An empty expr returns every row.
func Select(rows []map[string]string, expr string) []map[string]string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return append([]map[string]string(nil), rows...)
	}
	tok := strings.Fields(expr)
	var out []map[string]string
	for _, row := range rows {
		if parseTightOr(row, tok) {
			out = append(out, row)
		}
	}
	return out
}

// parseTightOr treats || as the inner operator, so && binds too loosely.
func parseTightOr(row map[string]string, tok []string) bool {
	left, rest := parseOr(row, tok)
	for len(rest) > 0 && rest[0] == "&&" {
		var right bool
		right, rest = parseOr(row, rest[1:])
		left = left && right
	}
	return left && len(rest) == 0
}

func parseOr(row map[string]string, tok []string) (bool, []string) {
	left, rest := parseCmp(row, tok)
	for len(rest) > 0 && rest[0] == "||" {
		var right bool
		right, rest = parseCmp(row, rest[1:])
		left = left || right
	}
	return left, rest
}

func parseCmp(row map[string]string, tok []string) (bool, []string) {
	if len(tok) < 3 {
		return false, nil
	}
	got := row[tok[0]]
	op, want := tok[1], tok[2]
	var ok bool
	switch op {
	case "=":
		ok = got == want
	case ">":
		ok = cmpNum(got, want) > 0
	case "<":
		ok = cmpNum(got, want) < 0
	}
	return ok, tok[3:]
}

func cmpNum(a, b string) int {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr != nil || berr != nil {
		return strings.Compare(a, b)
	}
	return ai - bi
}
`

const heavyQueryTest = `package p

import (
	"reflect"
	"testing"
)

func TestSelect(t *testing.T) {
	rows := []map[string]string{
		{"name": "ada", "role": "admin", "age": "10"},
		{"name": "bea", "role": "user", "age": "3"},
		{"name": "cy", "role": "user", "age": "21"},
	}
	if got := Select(rows, ""); len(got) != 3 {
		t.Fatalf("empty expr: %#v", got)
	}
	got := Select(rows, "age > 3")
	if len(got) != 2 || got[0]["name"] != "ada" || got[1]["name"] != "cy" {
		t.Fatalf("age > 3: %#v", got)
	}
	got = Select(rows, "role = user && age > 3")
	if len(got) != 1 || got[0]["name"] != "cy" {
		t.Fatalf("and: %#v", got)
	}
	// && binds tighter, so this is (name = nobody && role = admin) || age > 3.
	got = Select(rows, "name = nobody && role = admin || age > 3")
	want := []string{"ada", "cy"}
	var names []string
	for _, row := range got {
		names = append(names, row["name"])
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("precedence: %#v", names)
	}
	got = Select(rows, "age < 10 || role = admin")
	if len(got) != 2 {
		t.Fatalf("or: %#v", got)
	}
}
`

const heavyWidth = `package p

import "unicode/utf8"

// DisplayWidth counts ASCII runes as 1, other non-combining runes as 2, and
// combining marks U+0300..U+036F as 0.
func DisplayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

func runeWidth(r rune) int {
	if r <= 0x7F {
		return 1
	}
	if r >= 0x300 && r <= 0x36F {
		return 2
	}
	return 2
}

// keep utf8 imported so a byte-oriented rewrite still compiles against the
// same file layout the tests expect.
var _ = utf8.RuneCountInString
`

const heavyWidthTest = `package p

import "testing"

func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"abc", 3},
		{"é", 2},
		{"あ", 2},
		{"a\u0301", 1},
		{"é\u0301", 2},
		{"go 言語", 7},
	}
	for _, c := range cases {
		if got := DisplayWidth(c.s); got != c.want {
			t.Fatalf("DisplayWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}
`

const heavyStore = `package p

import "sort"

// Store is a sorted key/value map. Range returns keys in [start, end).
type Store struct {
	keys []string
	vals []string
}

func (s *Store) Set(k, v string) {
	i := sort.SearchStrings(s.keys, k)
	if i < len(s.keys) && s.keys[i] == k {
		s.vals[i] = v
		return
	}
	s.keys = append(s.keys, "")
	s.vals = append(s.vals, "")
	copy(s.keys[i+1:], s.keys[i:])
	copy(s.vals[i+1:], s.vals[i:])
	s.keys[i] = k
	s.vals[i] = v
}

func (s *Store) Get(k string) (string, bool) {
	i := sort.SearchStrings(s.keys, k)
	if i < len(s.keys) && s.keys[i] == k {
		return s.vals[i], true
	}
	return "", false
}

func (s *Store) Range(start, end string) []string {
	i := sort.SearchStrings(s.keys, start)
	j := sort.SearchStrings(s.keys, end)
	if j < len(s.keys) && s.keys[j] == end {
		j++
	}
	if i >= j {
		return nil
	}
	return append([]string(nil), s.keys[i:j]...)
}
`

const heavyStoreTest = `package p

import (
	"reflect"
	"testing"
)

func TestStore(t *testing.T) {
	var s Store
	s.Set("b", "1")
	s.Set("a", "2")
	s.Set("c", "3")
	s.Set("b", "9")
	if v, ok := s.Get("b"); !ok || v != "9" {
		t.Fatalf("Get b = %q %v", v, ok)
	}
	if _, ok := s.Get("z"); ok {
		t.Fatal("missing key found")
	}
	if got := s.Range("a", "c"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Range a,c = %#v", got)
	}
	if got := s.Range("a", "a"); got != nil {
		t.Fatalf("empty range %#v", got)
	}
	if got := s.Range("b", "z"); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("Range b,z = %#v", got)
	}
	if got := s.Range("", "b"); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("Range '',b = %#v", got)
	}
}
`
