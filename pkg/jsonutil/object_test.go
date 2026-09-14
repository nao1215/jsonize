package jsonutil

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestObjectOrderAndReplace(t *testing.T) {
	t.Parallel()
	o := NewObject()
	o.Set("b", int64(1))
	o.Set("a", "x")
	o.Set("b", int64(2))
	got, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"b":2,"a":"x"}` {
		t.Errorf("Marshal = %s", got)
	}
	v, ok := o.Get("a")
	if !ok || v != "x" {
		t.Errorf("Get(a) = %v, %v", v, ok)
	}
	if _, ok := o.Get("zz"); ok {
		t.Error("Get of missing key returned ok")
	}
	o.Delete("b")
	o.Delete("missing")
	if o.Len() != 1 || o.Members()[0].Key != "a" {
		t.Errorf("after Delete: %+v", o.Members())
	}
	var zero Object
	zero.Set("k", nil)
	if zero.Len() != 1 {
		t.Error("zero value Object should be usable")
	}
	var nilObj *Object
	if nilObj.Len() != 0 || nilObj.Members() != nil {
		t.Error("nil object")
	}
	if _, ok := nilObj.Get("a"); ok {
		t.Error("nil Get")
	}
	nilObj.Delete("a")
}

func TestEncodeValues(t *testing.T) {
	t.Parallel()
	o := NewObject()
	o.Set("s", "a<b>&")
	o.Set("i", int64(-5))
	o.Set("n", 7)
	o.Set("f", 1.5)
	o.Set("t", true)
	o.Set("z", nil)
	o.Set("arr", []any{"x", int64(1), nil})
	nested := NewObject()
	nested.Set("k", "v")
	o.Set("obj", nested)
	var nilObj *Object
	o.Set("nilobj", nilObj)
	o.Set("other", map[string]int{"m": 1})
	var buf bytes.Buffer
	if err := Encode(&buf, o, false); err != nil {
		t.Fatal(err)
	}
	want := `{"s":"a<b>&","i":-5,"n":7,"f":1.5,"t":true,"z":null,"arr":["x",1,null],"obj":{"k":"v"},"nilobj":null,"other":{"m":1}}` + "\n"
	if buf.String() != want {
		t.Errorf("Encode =\n%s\nwant\n%s", buf.String(), want)
	}
	buf.Reset()
	if err := Encode(&buf, o, true); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "{\n  \"s\"") {
		t.Errorf("pretty output = %q", buf.String())
	}
	if !json.Valid(buf.Bytes()) {
		t.Error("pretty output is not valid JSON")
	}
}

func TestEncodeNonFinite(t *testing.T) {
	t.Parallel()
	if _, err := Marshal(math.NaN()); err == nil {
		t.Error("NaN should fail")
	}
	if _, err := Marshal([]any{math.Inf(1)}); err == nil {
		t.Error("Inf should fail")
	}
	o := NewObject()
	o.Set("x", math.Inf(-1))
	if _, err := o.MarshalJSON(); err == nil {
		t.Error("object with Inf should fail")
	}
	if err := Encode(&bytes.Buffer{}, math.NaN(), true); err == nil {
		t.Error("Encode NaN should fail")
	}
	if _, err := Marshal(make(chan int)); err == nil {
		t.Error("unsupported type should fail")
	}
}

func TestMarshalJSONViaStdlib(t *testing.T) {
	t.Parallel()
	o := NewObject()
	o.Set("z", 1)
	o.Set("a", 2)
	b, err := json.Marshal(map[string]any{"o": o})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"o":{"z":1,"a":2}}` {
		t.Errorf("json.Marshal = %s", b)
	}
}

// TestEncodeAgreesWithStdlib pins the hand-written encoding to what
// encoding/json writes with HTML escaping off, over the strings and
// numbers that exercise every branch of it, and the indented shape to
// what json.Indent makes of the compact one.
func TestEncodeAgreesWithStdlib(t *testing.T) {
	t.Parallel()
	strs := []string{
		"", "plain", "quote\"back\\slash", "tab\tnl\nret\rbell\a", "\x00\x1f\x7f",
		"a<b>&c", "é日本語😀", "line\xe2\x80\xa8sep\xe2\x80\xa9",
	}
	floats := []float64{0, -0, 1, 1.5, -2.25, 1e20, 1e21, 1e-6, 1e-7, 123456789.125, 5e-324, math.MaxFloat64, 0.1, 1e100}
	values := make([]any, 0, len(strs)+len(floats)+6)
	for _, s := range strs {
		values = append(values, s)
	}
	for _, f := range floats {
		values = append(values, f)
	}
	values = append(values, int64(math.MinInt64), int64(math.MaxInt64), 0, true, false, nil)
	for _, v := range values {
		got, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal(%#v): %v", v, err)
		}
		var std bytes.Buffer
		enc := json.NewEncoder(&std)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
		if want := strings.TrimSuffix(std.String(), "\n"); string(got) != want {
			t.Errorf("Marshal(%#v) = %s, encoding/json writes %s", v, got, want)
		}
	}
	// A byte that is not UTF-8 is written as the escape of U+FFFD.
	// encoding/json wrote that escape up to Go 1.26 and writes the
	// character itself after, and the two read back the same; jz writes
	// the escape so that its output does not depend on the Go it was
	// built with.
	escape := `\u` + "fffd"
	for in, want := range map[string]string{
		"bad\xffutf8\xc3": `"bad` + escape + `utf8` + escape + `"`,
		"\xed\xa0\x80":    `"` + escape + escape + escape + `"`,
	} {
		got, err := Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("Marshal(%q) = %s, want %s", in, got, want)
		}
		var back string
		if err := json.Unmarshal(got, &back); err != nil || back != string([]rune(in)) {
			t.Errorf("Marshal(%q) reads back as %q, %v", in, back, err)
		}
	}
	o := NewObject()
	o.Set("empty_obj", NewObject())
	o.Set("empty_arr", []any{})
	o.Set("list", []any{int64(1), NewObject(), []any{"x"}})
	o.Set("other", map[string]any{"k": []int{1, 2}})
	o.Set("s", "v")
	compact, err := Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Indent(&want, compact, "", "  "); err != nil {
		t.Fatal(err)
	}
	want.WriteByte('\n')
	var got bytes.Buffer
	if err := Encode(&got, o, true); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("pretty Encode =\n%s\njson.Indent writes\n%s", got.String(), want.String())
	}
}

// TestObjectIndex exercises an object past the size at which it keeps an
// index, through every operation that has to keep the index right.
func TestObjectIndex(t *testing.T) {
	t.Parallel()
	o := NewObject()
	for i := range 2 * indexAt {
		o.Set(string(rune('a'+i)), i)
	}
	if o.index == nil {
		t.Fatal("no index over a large object")
	}
	o.Set("c", "replaced")
	if v, _ := o.Get("c"); v != "replaced" || o.Members()[2].Key != "c" {
		t.Errorf("replace moved or lost c: %v", o.Members())
	}
	o.Delete("b")
	if v, ok := o.Get("d"); !ok || v != 3 || o.Members()[2].Key != "d" {
		t.Errorf("after Delete: d = %v, %v; members %v", v, ok, o.Members())
	}
	if _, ok := o.Get("b"); ok {
		t.Error("deleted key still found")
	}
	o.Set("new", true)
	if v, ok := o.Get("new"); !ok || v != true {
		t.Errorf("Set after Delete: %v, %v", v, ok)
	}
}
