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
