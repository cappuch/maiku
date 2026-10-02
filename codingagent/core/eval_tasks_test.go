package core

func codingTasks() []codingTask {
	return []codingTask{
		task("merge", "merge.go", mergeImpl, "merge_test.go", mergeTest),
		task("wrap", "wrap.go", wrapImpl, "wrap_test.go", wrapTest),
		task("eval", "eval.go", evalImpl, "eval_test.go", evalTest),
		task("braces", "braces.go", bracesImpl, "braces_test.go", bracesTest),
		task("semver", "semver.go", semverImpl, "semver_test.go", semverTest),
		task("fields", "fields.go", fieldsImpl, "fields_test.go", fieldsTest),
		task("order", "order.go", orderImpl, "order_test.go", orderTest),
		task("rle", "rle.go", rleImpl, "rle_test.go", rleTest),
		task("clean", "clean.go", cleanImpl, "clean_test.go", cleanTest),
		task("limit", "limit.go", limitImpl, "limit_test.go", limitTest),
	}
}

func task(name, srcName, src, testName, test string) codingTask {
	return codingTask{name: name, files: map[string]string{srcName: src, testName: test}}
}

const mergeImpl = `package p

import "sort"

func Merge(xs [][2]int) [][2]int {
	out := append([][2]int(nil), xs...)
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}
`

const mergeTest = `package p

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	cases := []struct {
		in, want [][2]int
	}{
		{nil, nil},
		{[][2]int{{1, 3}}, [][2]int{{1, 3}}},
		{[][2]int{{1, 4}, {2, 5}, {7, 8}}, [][2]int{{1, 5}, {7, 8}}},
		{[][2]int{{1, 2}, {2, 3}}, [][2]int{{1, 3}}},
		{[][2]int{{5, 6}, {1, 2}}, [][2]int{{1, 2}, {5, 6}}},
		{[][2]int{{1, 10}, {2, 3}, {4, 5}}, [][2]int{{1, 10}}},
		{[][2]int{{1, 2}, {4, 5}}, [][2]int{{1, 2}, {4, 5}}},
	}
	for _, c := range cases {
		got := Merge(append([][2]int(nil), c.in...))
		if len(c.want) == 0 {
			if len(got) != 0 {
				t.Fatalf("%v -> %v", c.in, got)
			}
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%v -> %v, want %v", c.in, got, c.want)
		}
	}
}
`

const wrapImpl = `package p

import "strings"

func Wrap(s string, width int) string {
	if width <= 0 || len(s) <= width {
		return s
	}
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteByte('\n')
		s = s[width:]
	}
	b.WriteString(s)
	return b.String()
}
`

const wrapTest = `package p

import "testing"

func TestWrap(t *testing.T) {
	cases := []struct{ in string; width int; want string }{
		{"hello world", 20, "hello world"},
		{"hello world", 5, "hello\nworld"},
		{"a bb ccc", 3, "a\nbb\nccc"},
		{"toolong", 3, "toolong"},
		{"", 5, ""},
		{"ab cd", 5, "ab cd"},
		{"one two three", 7, "one two\nthree"},
	}
	for _, c := range cases {
		if got := Wrap(c.in, c.width); got != c.want {
			t.Fatalf("Wrap(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}
`

const evalImpl = `package p

import (
	"strconv"
	"strings"
	"unicode"
)

func Eval(s string) (int, error) {
	var nums []int
	var ops []byte
	i := 0
	for i < len(s) {
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return 0, strconv.ErrSyntax
		}
		n, err := strconv.Atoi(s[i:j])
		if err != nil {
			return 0, err
		}
		nums = append(nums, n)
		i = j
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] != '+' && s[i] != '*' {
			return 0, strconv.ErrSyntax
		}
		ops = append(ops, s[i])
		i++
	}
	if len(nums) == 0 || len(ops) != len(nums)-1 {
		return 0, strconv.ErrSyntax
	}
	acc := nums[0]
	for k, op := range ops {
		if op == '+' {
			acc += nums[k+1]
		} else {
			acc *= nums[k+1]
		}
	}
	_ = strings.TrimSpace
	return acc, nil
}
`

const evalTest = `package p

import "testing"

func TestEval(t *testing.T) {
	cases := []struct {
		in   string
		want int
		bad  bool
	}{
		{"2+3*4", 14, false},
		{"2*3+4", 10, false},
		{"7", 7, false},
		{"2 + 3", 5, false},
		{"2*", 0, true},
		{"", 0, true},
		{"2++3", 0, true},
	}
	for _, c := range cases {
		got, err := Eval(c.in)
		if c.bad {
			if err == nil {
				t.Fatalf("Eval(%q) err = nil", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Fatalf("Eval(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}
`

const bracesImpl = `package p

func Balanced(s string) bool {
	n := 0
	for _, r := range s {
		switch r {
		case '(':
			n++
		case ')':
			n--
		}
		if n < 0 {
			return false
		}
	}
	return n == 0
}
`

const bracesTest = `package p

import "testing"

func TestBalanced(t *testing.T) {
	good := []string{"", "()", "([])", "([{}])", "a(b[c]{d})e", "()[]{}"}
	bad := []string{")", "(", "([)]", "(]", "([)", "(()", "}{"}
	for _, s := range good {
		if !Balanced(s) {
			t.Fatalf("Balanced(%q) = false", s)
		}
	}
	for _, s := range bad {
		if Balanced(s) {
			t.Fatalf("Balanced(%q) = true", s)
		}
	}
}
`

const semverImpl = `package p

func Less(a, b string) bool { return a < b }
`

const semverTest = `package p

import "testing"

func TestLess(t *testing.T) {
	if !Less("1.9", "1.10") {
		t.Fatal("1.9 should be less than 1.10")
	}
	if Less("1.10", "1.9") {
		t.Fatal("1.10 should not be less than 1.9")
	}
	if Less("1.0", "1.0.0") || Less("1.0.0", "1.0") {
		t.Fatal("1.0 and 1.0.0 are equal")
	}
	if Less("2.0.0", "1.9.9") {
		t.Fatal("2.0.0 is not less than 1.9.9")
	}
	if !Less("1.2.3", "1.2.4") {
		t.Fatal("1.2.3 should be less than 1.2.4")
	}
	if Less("1.2.3", "1.2.3") {
		t.Fatal("equal versions are not ordered")
	}
}
`

const fieldsImpl = `package p

import "strings"

func Fields(s string) []string { return strings.Split(s, ",") }
`

const fieldsTest = `package p

import (
	"reflect"
	"testing"
)

func TestFields(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{"a,\"b,c\",d", []string{"a", "b,c", "d"}},
		{"a,\"b\"\"c\"", []string{"a", "b\"c"}},
		{"a,,b", []string{"a", "", "b"}},
		{"", []string{""}},
		{"\"a,b\"", []string{"a,b"}},
	}
	for _, c := range cases {
		got := Fields(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("Fields(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
`

const orderImpl = `package p

func Order(nodes []string, edges [][2]string) ([]string, error) {
	return append([]string(nil), nodes...), nil
}
`

const orderTest = `package p

import (
	"reflect"
	"testing"
)

func TestOrder(t *testing.T) {
	got, err := Order([]string{"a", "b", "c"}, [][2]string{{"a", "b"}, {"b", "c"}})
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("chain %v %v", got, err)
	}
	got, err = Order([]string{"a", "b", "c", "d"}, [][2]string{{"a", "c"}, {"b", "c"}, {"c", "d"}})
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("diamond %v %v", got, err)
	}
	got, err = Order([]string{"c", "a", "b"}, nil)
	if err != nil || !reflect.DeepEqual(got, []string{"c", "a", "b"}) {
		t.Fatalf("stable %v %v", got, err)
	}
	if _, err = Order([]string{"a", "b"}, [][2]string{{"a", "b"}, {"b", "a"}}); err == nil {
		t.Fatal("cycle should error")
	}
}
`

const rleImpl = `package p

import "strconv"

func Encode(s string) string {
	if s == "" {
		return ""
	}
	var b []byte
	n := 1
	for i := 1; i < len(s); i++ {
		if s[i] == s[i-1] {
			n++
			continue
		}
		b = append(b, s[i-1])
		b = append(b, strconv.Itoa(n)...)
		n = 1
	}
	return string(b)
}
`

const rleTest = `package p

import "testing"

func TestEncode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"a", "a1"},
		{"aaabbc", "a3b2c1"},
		{"abc", "a1b1c1"},
		{"zzzz", "z4"},
	}
	for _, c := range cases {
		if got := Encode(c.in); got != c.want {
			t.Fatalf("Encode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
`

const cleanImpl = `package p

import "strings"

func Clean(p string) string { return strings.ReplaceAll(p, "//", "/") }
`

const cleanTest = `package p

import "testing"

func TestClean(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a/b/../c", "a/c"},
		{"./a", "a"},
		{"a//b", "a/b"},
		{"../a", "../a"},
		{"a/../../b", "../b"},
		{"a/b/.", "a/b"},
		{"", "."},
		{"/", "/"},
		{"a/b/../../..", ".."},
	}
	for _, c := range cases {
		if got := Clean(c.in); got != c.want {
			t.Fatalf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
`

const limitImpl = `package p

type Limiter struct {
	n, used int
}

func NewLimiter(n int) *Limiter { return &Limiter{n: n} }

func (l *Limiter) Allow(t int) bool {
	if l.used >= l.n {
		return false
	}
	l.used++
	return true
}
`

const limitTest = `package p

import "testing"

// Allow accepts an event at time t when fewer than n events lie in (t-10, t].
func TestAllow(t *testing.T) {
	l := NewLimiter(2)
	if !l.Allow(0) || !l.Allow(1) || l.Allow(2) {
		t.Fatal("burst of 2")
	}
	if !l.Allow(11) {
		t.Fatal("event at 0 has left the window by t=11")
	}
	if l.Allow(12) {
		t.Fatal("event at 1 is still inside (2, 12]")
	}
	l2 := NewLimiter(1)
	if !l2.Allow(5) || l2.Allow(14) || !l2.Allow(15) {
		t.Fatal("window edge: 5 counts at 14 and not at 15")
	}
}
`
