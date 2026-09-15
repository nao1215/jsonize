package engine

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/nao1215/jsonize/pkg/definition"
	"github.com/nao1215/jsonize/pkg/jsonutil"
)

func load(t *testing.T, src string) *definition.Definition {
	t.Helper()
	d, err := definition.Load([]byte(src), "test.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := jsonutil.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const dfDef = `
format: 1
command: df
variant: gnu
input:
  ignore: ['^df: ']
parse:
  type: table
  header:
    columns: [filesystem, 1k_blocks, used, available, use_percent, mounted_on]
fields:
  1k_blocks: {type: int}
  used: {type: int}
  available: {type: int}
  use_percent: {type: int, trim_suffix: "%", null_if: ["-"]}
`

const dfInput = `Filesystem              1K-blocks    Used Available Use% Mounted on
devtmpfs                  1918816       0   1918816   0% /dev
df: /run/user/1000/gvfs: Permission denied
/dev/mapper/centos-root  17811456 1805580  16005876  11% /
tmpfs                      386136       0    386136   -  /run/user 1000

`

func TestParseTableWhitespace(t *testing.T) {
	t.Parallel()
	got, err := Parse(load(t, dfDef), []byte(dfInput), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"filesystem":"devtmpfs","1k_blocks":1918816,"used":0,"available":1918816,"use_percent":0,"mounted_on":"/dev"},` +
		`{"filesystem":"/dev/mapper/centos-root","1k_blocks":17811456,"used":1805580,"available":16005876,"use_percent":11,"mounted_on":"/"},` +
		`{"filesystem":"tmpfs","1k_blocks":386136,"used":0,"available":386136,"use_percent":null,"mounted_on":"/run/user 1000"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
}

func TestParseTableDerivedHeaderAndRaw(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: free
variant: gnu
parse:
  type: table
  header:
    leading_label: type
    rename: {buff_cache: cache}
  min_fields: 4
fields:
  type: {trim_suffix: ":"}
  total: {type: int}
  cache: {type: int}
`)
	input := "              total        used        free      shared  buff/cache   available\nMem:        3861332      222820     3364176       11832      274336     3389588\nSwap:       2097148           0     2097148\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"Mem","total":3861332,"used":"222820","free":"3364176","shared":"11832","cache":274336,"available":"3389588"},` +
		`{"type":"Swap","total":2097148,"used":"0","free":"2097148","shared":null,"cache":null,"available":null}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// Swap has only 4 cells; min_fields defaults to column count so it
	// must fail unless min_fields is lowered.
	_, err = Parse(load(t, `
format: 1
command: free
variant: strict
parse:
  type: table
  header: {leading_label: type}
`), []byte(input), Options{})
	if err == nil || !strings.Contains(err.Error(), "expected at least 7 fields") {
		t.Fatalf("expected field count error, got %v", err)
	}
}

// A table lines its columns up in the columns of a terminal, where a
// kana or a CJK character takes two and a combining accent none. Counting
// characters instead put the rest of such a row into the first cell.
func TestParseTableAlignedByDisplayWidth(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned}\n")
	input := "WHO                  UID  USER\n" +
		"ModemManager         0    root\n" +
		"スクリーンロッカー   1000 user01\n" +
		"é́́́́                    1000 user01\n" +
		"ｆｕｌｌ             7    x\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"who":"ModemManager","uid":"0","user":"root"},` +
		`{"who":"スクリーンロッカー","uid":"1000","user":"user01"},` +
		`{"who":"e` + strings.Repeat("́", 5) + `","uid":"1000","user":"user01"},` +
		`{"who":"ｆｕｌｌ","uid":"7","user":"x"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// A header in a wide script is measured the same way.
	named := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned, header: {columns: [name, value, x]}}\n")
	got, err = Parse(named, []byte("名前名前名前 値 X\na            1  z\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `[{"name":"a","value":"1","x":"z"}]` {
		t.Error(mustJSON(t, got))
	}
}

func TestParseTableAligned(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: lsblk
variant: linux
parse:
  type: table
  split: aligned
fields:
  rm: {type: bool}
  ro: {type: bool}
`)
	input := "NAME            MAJ:MIN RM  SIZE RO TYPE MOUNTPOINT\n" +
		"sda               8:0    0   20G  0 disk \n" +
		"├─sda1            8:1    0    1G  0 part /boot\n" +
		"  ├─centos-root 253:0    0   17G  0 lvm  /\n" +
		"loop9             7:9    0 123.4M  1 loop /snap/x y\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"sda","maj_min":"8:0","rm":false,"size":"20G","ro":false,"type":"disk","mountpoint":null},` +
		`{"name":"├─sda1","maj_min":"8:1","rm":false,"size":"1G","ro":false,"type":"part","mountpoint":"/boot"},` +
		`{"name":"├─centos-root","maj_min":"253:0","rm":false,"size":"17G","ro":false,"type":"lvm","mountpoint":"/"},` +
		`{"name":"loop9","maj_min":"7:9","rm":false,"size":"123.4M","ro":true,"type":"loop","mountpoint":"/snap/x y"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// aligned with explicit columns requires the same count as the header
	_, err = Parse(load(t, `
format: 1
command: lsblk
variant: bad
parse:
  type: table
  split: aligned
  header: {columns: [a, b, c, d, e, f, g, h]}
`), []byte(input), Options{})
	if err == nil || !strings.Contains(err.Error(), "header has 7 columns but the definition declares 8") {
		t.Fatalf("expected column mismatch, got %v", err)
	}
	// Fewer declared columns than header tokens: trailing tokens merge into the last column.
	got, err = Parse(load(t, `
format: 1
command: lsblk
variant: merged
parse:
  type: table
  split: aligned
  header: {columns: [a, b, c, d, e, rest]}
`), []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustJSON(t, got), `"rest":"loop /snap/x y"`) {
		t.Error(mustJSON(t, got))
	}
	// explicit columns with matching count works
	got, err = Parse(load(t, `
format: 1
command: lsblk
variant: cols
parse:
  type: table
  split: aligned
  header: {columns: [a, b, c, d, e, f, g]}
`), []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustJSON(t, got), `"g":"/snap/x y"`) {
		t.Error(mustJSON(t, got))
	}
}

func TestParseTableDelimiterNoHeader(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: du
variant: posix
parse:
  type: table
  split: delimiter
  delimiter: "\t"
  header:
    none: true
    columns: [size, name]
fields:
  size: {type: int}
`)
	got, err := Parse(def, []byte("134164\t/usr/bin\n0\t/usr/lib/debug\twith tab\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"size":134164,"name":"/usr/bin"},{"size":0,"name":"/usr/lib/debug\twith tab"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	_, err = Parse(def, []byte("134164\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "line 1: expected at least 2 fields") {
		t.Errorf("expected count error, got %v", err)
	}
	// delimiter with header line and too many fields
	def2 := load(t, `
format: 1
command: csv
variant: x
parse:
  type: table
  split: delimiter
  delimiter: ","
  max_fields: 5
  min_fields: 1
`)
	_, err = Parse(def2, []byte("a,b\n1,2,3\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "expected at most 2 fields but found 3") {
		t.Errorf("expected too-many error, got %v", err)
	}
	got, err = Parse(def2, []byte("a,b\n1\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `[{"a":"1","b":null}]` {
		t.Error(mustJSON(t, got))
	}
}

func TestParseTableEmptyAndHeaderErrors(t *testing.T) {
	t.Parallel()
	def := load(t, dfDef)
	got, err := Parse(def, nil, Options{})
	if err != nil || mustJSON(t, got) != "[]" {
		t.Errorf("empty input: %v %v", got, err)
	}
	got, err = Parse(def, []byte("Filesystem 1K-blocks Used Available Use% Mounted on\n"), Options{})
	if err != nil || mustJSON(t, got) != "[]" {
		t.Errorf("header only: %v %v", got, err)
	}
	derived := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table}\ninput: {skip_blank: false}\n")
	// Blank lines before the text are skipped whatever skip_blank says, so
	// the blank header is the one that follows the heading select.after
	// names.
	afterHeading := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table}\ninput: {skip_blank: false, select: {after: '^start$'}}\n")
	_, err = Parse(afterHeading, []byte("start\n   \nx\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "header line is empty") {
		t.Errorf("empty header: %v", err)
	}
	_, err = Parse(derived, []byte("a a\n1 2\n"), Options{}) //nolint:dupword // duplicate header names on purpose
	if err == nil || !strings.Contains(err.Error(), "duplicate column") {
		t.Errorf("dup header: %v", err)
	}
	_, err = Parse(derived, []byte("a -- b\n1 2\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "normalises to an empty name") {
		t.Errorf("empty name: %v", err)
	}
	many := strings.Repeat("c ", definition.MaxColumns+1)
	_, err = Parse(derived, []byte(many+"\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Errorf("too many columns: %v", err)
	}
}

func TestParseRegexEachLine(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: mount
variant: linux
parse:
  type: regex
  pattern: '^(?P<filesystem>.+?) on (?P<mount_point>.+?) type (?P<type>\S+) \((?P<options>[^)]*)\)$'
fields:
  options: {type: array, split: ","}
`)
	input := "sysfs on /sys type sysfs (rw,nosuid)\nproc on /proc type proc ()\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"filesystem":"sysfs","mount_point":"/sys","type":"sysfs","options":["rw","nosuid"]},{"filesystem":"proc","mount_point":"/proc","type":"proc","options":[]}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	_, err = Parse(def, []byte("garbage line\n"), Options{})
	var pe *ParseError
	if !errors.As(err, &pe) || pe.Line != 1 || !strings.Contains(err.Error(), "does not match pattern") {
		t.Errorf("mismatch error = %v", err)
	}
	// A part says which lines of its region belong to a sibling, and only
	// those are dropped: anything else still has to be read.
	shared := load(t, `
format: 1
command: mount
variant: shared
parse:
  type: composite
  parts:
    - name: letters
      ignore: ['^\d']
      parse:
        type: regex
        pattern: '^(?P<a>[a-z])$'
    - name: numbers
      ignore: ['^[a-z]']
      parse:
        type: regex
        pattern: '^(?P<n>\d+)$'
`)
	got, err = Parse(shared, []byte("x\n1\ny\n2\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"letters":[{"a":"x"},{"a":"y"}],"numbers":[{"n":"1"},{"n":"2"}]}` {
		t.Errorf("part ignore: %v %v", mustJSON(t, got), err)
	}
	_, err = Parse(shared, []byte("x\n1\n!\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "does not match pattern") {
		t.Errorf("a line no part claims must fail: %v", err)
	}
}

func TestParseRegexInputAndNested(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: id
variant: posix
parse:
  type: regex
  each: input
  pattern: '^uid=(?P<uid>\S+) gid=(?P<gid>\S+) groups=(?P<groups>\S+)(?: context=(?P<context>\S+))?\s*$'
fields:
  uid:
    type: object
    regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$'
    fields:
      id: {type: int}
  gid:
    type: object
    regex: '^(?P<id>\d+)(?:\((?P<name>[^)]*)\))?$'
    fields:
      id: {type: int}
      name: {when_missing: omit}
  groups:
    type: array
    split: ","
    items:
      type: object
      regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$'
      fields:
        id: {type: int}
  context:
    type: object
    when_missing: omit
    regex: '^(?P<user>[^:]+):(?P<role>[^:]+):(?P<type>[^:]+):(?P<level>.+)$'
`)
	got, err := Parse(def, []byte("uid=1000(kbrazil) gid=1000(kbrazil) groups=1000(kbrazil),10(wheel) context=unconfined_u:unconfined_r:unconfined_t:s0-s0:c0.c1023\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"uid":{"id":1000,"name":"kbrazil"},"gid":{"id":1000,"name":"kbrazil"},"groups":[{"id":1000,"name":"kbrazil"},{"id":10,"name":"wheel"}],"context":{"user":"unconfined_u","role":"unconfined_r","type":"unconfined_t","level":"s0-s0:c0.c1023"}}`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	got, err = Parse(def, []byte("uid=501(k) gid=20 groups=20(staff)\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"uid":{"id":501,"name":"k"},"gid":{"id":20},"groups":[{"id":20,"name":"staff"}]}`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	_, err = Parse(def, []byte("nothing here\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "input does not match pattern") {
		t.Errorf("expected input mismatch, got %v", err)
	}
	_, err = Parse(def, []byte("uid=abc gid=1 groups=1(a)\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), `field "uid": value "abc" does not match`) {
		t.Errorf("expected object mismatch, got %v", err)
	}
	_, err = Parse(def, []byte("uid=1(a) gid=1 groups=x(a)\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), `field "groups"`) {
		t.Errorf("expected item error, got %v", err)
	}
}

func TestParseKV(t *testing.T) {
	t.Parallel()
	list := load(t, `
format: 1
command: env
variant: posix
parse:
  type: kv
fields:
  SHLVL: {type: int}
`)
	input := "HOME=/root\nEMPTY=\nSHLVL=2\nWITH=a=b\n"
	got, err := Parse(list, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"HOME","value":"/root"},{"name":"EMPTY","value":""},{"name":"SHLVL","value":2},{"name":"WITH","value":"a=b"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	_, err = Parse(list, []byte("no separator\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), `line 1: expected "key=value"`) {
		t.Errorf("mismatch: %v", err)
	}
	_, err = Parse(list, []byte("=novalue\n"), Options{})
	if err == nil {
		t.Error("empty key should fail")
	}
	m := load(t, `
format: 1
command: props
variant: colon
parse:
  type: kv
  separator: ":"
  as: map
fields:
  count: {type: int}
  flag: {type: bool}
`)
	got, err = Parse(m, []byte("name : jsonize\ncount: 3\nflag: yes\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `{"name":"jsonize","count":3,"flag":true}` {
		t.Error(mustJSON(t, got))
	}
	// A key printed twice would leave one of its values out of the
	// object, or fold a second copy of the document into the first, so
	// it is refused whatever the values are, and both places are named.
	for _, input := range []string{
		"name : jsonize\ncount: 3\nflag: yes\nname: last wins\n",
		"name : jsonize\ncount: 3\nflag: yes\nname: jsonize\n",
	} {
		_, err = Parse(m, []byte(input), Options{})
		var pe *ParseError
		if !errors.As(err, &pe) || pe.Line != 4 || pe.Field != "name" || !strings.Contains(err.Error(), "line 1") {
			t.Errorf("a repeated key in %q: %v", input, err)
		}
	}
}

// input.fold joins a wrapped continuation onto the line above it, which
// is how a report that breaks a long value at the terminal width is read
// as one value rather than as a label of its own.
func TestInputFold(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: x
variant: wrapped
input:
  fold: '^[ \t]+\S'
parse:
  type: kv
  separator: ":"
  as: map
`)
	got, err := Parse(def, []byte("modes:  10baseT/Half\n        100baseT/Full\nduplex: Full\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `{"modes":"10baseT/Half 100baseT/Full","duplex":"Full"}` {
		t.Error(mustJSON(t, got))
	}

	// A continuation with nothing above it is reported, not dropped.
	if _, err := Parse(def, []byte("   orphan\nmodes: x\n"), Options{}); err == nil {
		t.Error("a continuation on the first line should be an error")
	} else if !strings.Contains(err.Error(), "nothing to join") {
		t.Error(err)
	}
}

// Folding runs before ignoring, so a continuation reaches the line it
// belongs to even where that line is dropped, and a continuation of a
// dropped line goes with it.
func TestInputFoldRunsBeforeIgnore(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: x
variant: wrapped
input:
  fold: '^[ \t]+\S'
  ignore: ['^note:']
parse:
  type: kv
  separator: ":"
  as: map
`)
	got, err := Parse(def, []byte("note:  dropped\n       with its continuation\nkept:  value\n       and its own\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `{"kept":"value and its own"}` {
		t.Error(mustJSON(t, got))
	}
}

func TestParseRecords(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: x
variant: blocks
parse:
  type: records
  start: '^\S'
  parts:
    - name: head
      select: {limit: 1}
      parse:
        type: regex
        each: input
        pattern: '^(?P<name>\S+)$'
    - name: items
      select: {skip: 1}
      parse:
        type: regex
        pattern: '^\s+(?P<item>\S+)$'
`)
	got, err := Parse(def, []byte("alpha\n  one\n  two\nbeta\n  three\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"head":{"name":"alpha"},"items":[{"item":"one"},{"item":"two"}]},` +
		`{"head":{"name":"beta"},"items":[{"item":"three"}]}]`
	if mustJSON(t, got) != want {
		t.Error(mustJSON(t, got))
	}

	// A record with no children still produces the record.
	got, err = Parse(def, []byte("alpha\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"head":{"name":"alpha"},"items":[]}]` {
		t.Errorf("childless record: %v %v", mustJSON(t, got), err)
	}

	// Text before the first record is reported rather than dropped.
	if _, err := Parse(def, []byte("  stray\nalpha\n"), Options{}); err == nil {
		t.Error("a line before the first record should be an error")
	} else if !strings.Contains(err.Error(), "precedes the first record") {
		t.Error(err)
	}
}

// A composite part may be records: a report that opens with a banner and
// then repeats a block needs one part for the banner and one for the
// blocks.
func TestParseCompositeWithRecordsPart(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: x
variant: banner-and-blocks
parse:
  type: composite
  parts:
    - name: header
      select: {limit: 1}
      parse:
        type: regex
        each: input
        pattern: '^report for (?P<host>\S+)$'
    - name: entries
      select: {skip: 1}
      parse:
        type: records
        start: '^\S'
        parts:
          - name: head
            select: {limit: 1}
            parse:
              type: regex
              each: input
              pattern: '^(?P<name>\S+)$'
          - name: items
            select: {skip: 1}
            parse:
              type: regex
              pattern: '^\s+(?P<item>\S+)$'
`)
	got, err := Parse(def, []byte("report for alpha\nfirst\n  one\n  two\nsecond\n  three\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"header":{"host":"alpha"},"entries":[` +
		`{"head":{"name":"first"},"items":[{"item":"one"},{"item":"two"}]},` +
		`{"head":{"name":"second"},"items":[{"item":"three"}]}]}`
	if mustJSON(t, got) != want {
		t.Error(mustJSON(t, got))
	}
}

func TestParseComposite(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: w
variant: linux
parse:
  type: composite
  parts:
    - name: uptime
      select: {limit: 1}
      parse:
        type: regex
        each: input
        pattern: '^\s*(?P<time>\S+)\s+up\s+(?P<uptime>.+?),\s+(?P<users>\d+) users?,\s+load average: (?P<load_1m>[\d.]+), (?P<load_5m>[\d.]+), (?P<load_15m>[\d.]+)$'
      fields:
        users: {type: int}
        load_1m: {type: float}
        load_5m: {type: float}
        load_15m: {type: float}
    - name: users
      select: {skip: 1}
      parse:
        type: table
        split: aligned
        header:
          rename: {login: login_at}
      fields:
        from: {null_if: ["-"]}
`)
	input := " 10:25:20 up 16:03,  2 users,  load average: 0.00, 0.01, 0.05\n" +
		"USER     TTY      FROM             LOGIN@   IDLE   JCPU   PCPU WHAT\n" +
		"kbrazil  ttyS0                     Fri18   32:16   2.66s  2.66s -bash\n" +
		"kbrazil  pts/0    192.168.71.1     09:53    8.00s  0.10s  0.00s w\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"uptime":{"time":"10:25:20","uptime":"16:03","users":2,"load_1m":0,"load_5m":0.01,"load_15m":0.05},` +
		`"users":[{"user":"kbrazil","tty":"ttyS0","from":null,"login_at":"Fri18","idle":"32:16","jcpu":"2.66s","pcpu":"2.66s","what":"-bash"},` +
		`{"user":"kbrazil","tty":"pts/0","from":"192.168.71.1","login_at":"09:53","idle":"8.00s","jcpu":"0.10s","pcpu":"0.00s","what":"w"}]}`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	_, err = Parse(def, []byte("bogus\nUSER TTY\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), `part "uptime"`) {
		t.Errorf("expected part error, got %v", err)
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()
	// The steps apply in the order after, until, skip, limit. What the
	// region leaves out is read by the second part, so the reading is
	// complete and the region is what the first part reports.
	def := load(t, `
format: 1
command: sec
variant: v
parse:
  type: composite
  parts:
    - name: region
      select:
        after: '^BEGIN$'
        until: '^END'
        skip: 1
        limit: 2
      parse: {type: regex, pattern: '^(?P<v>.+)$'}
    - name: rest
      ignore: ['^BEGIN$', '^[ab]$']
      parse: {type: regex, pattern: '^(?P<v>.+)$'}
`)
	got, err := Parse(def, []byte("junk\nBEGIN\nskipme\na\nb\nc\nEND\nafter\n"), Options{})
	want := `{"region":[{"v":"a"},{"v":"b"}],"rest":[{"v":"junk"},{"v":"skipme"},{"v":"c"},{"v":"END"},{"v":"after"}]}`
	if err != nil || mustJSON(t, got) != want {
		t.Errorf("select: %v %v", mustJSON(t, got), err)
	}
	got, err = Parse(def, []byte("no begin marker\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"region":[],"rest":[{"v":"no begin marker"}]}` {
		t.Errorf("missing after: %v %v", mustJSON(t, got), err)
	}
}

// A selection at the top level has no sibling to hand what it leaves out
// to, so everything it cuts off is text the definition did not read. The
// lines are named, first one first, rather than the result coming back
// shorter than the input.
func TestSelectLeavesNothingUnread(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: sec
variant: v
input:
  select:
    after: '^BEGIN'
    until: '^END'
    skip: 1
    limit: 2
parse:
  type: regex
  pattern: '^(?P<v>.+)$'
`)
	tests := []struct {
		name, input  string
		first, total int
	}{
		// END is the whole of what until states, so it is read; "after"
		// past it is not.
		{"before, skipped, past the limit and after the end", "junk\nBEGIN\nskipme\na\nb\nc\nEND\nafter\n", 1, 4},
		{"no heading at all", "no begin marker\n", 1, 1},
		{"skip beyond the end", "BEGIN\nonly\n", 2, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(def, []byte(tt.input), Options{})
			var ue *UnreadError
			if !errors.As(err, &ue) || !errors.Is(err, ErrUnread) {
				t.Fatalf("want an unread error, got %v", err)
			}
			var pe *ParseError
			if !errors.As(err, &pe) || pe.Line != tt.first || ue.Total != tt.total {
				t.Errorf("want %d unread from line %d, got %v", tt.total, tt.first, err)
			}
		})
	}
	// Blank lines are not text anyone could be missing, and the heading
	// the expression states whole counts as read.
	got, err := Parse(def, []byte("BEGIN\nskip\n\n"), Options{})
	if err == nil {
		t.Errorf("a skipped line was left unread and the parse still succeeded: %v", mustJSON(t, got))
	}
	got, err = Parse(def, []byte("BEGIN\n\n\n"), Options{})
	if err != nil || mustJSON(t, got) != `[]` {
		t.Errorf("blank lines only: %v %v", mustJSON(t, got), err)
	}
}

func TestConversionsAndRequired(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: t
variant: v
parse:
  type: table
  header: {none: true, columns: [i, f, b, s, r, n, o]}
  min_fields: 1
fields:
  i: {type: int}
  f: {type: float}
  b: {type: bool, true_values: [up], false_values: [down]}
  s: {type: duration, layout: mm:ss}
  r: {required: true}
  n: {null_if: ["-"], required: true}
  o: {when_missing: omit}
`)
	got, err := Parse(def, []byte("1 2.5 up 4:50 x y\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `[{"i":1,"f":2.5,"b":true,"s":290,"r":"x","n":"y"}]` {
		t.Error(mustJSON(t, got))
	}
	cases := map[string]string{
		"x 2.5 up 4:50 x y":  `field "i": cannot convert "x" to int`,
		"1 x up 4:50 x y":    `field "f"`,
		"1 2.5 maybe 4:50 x": `field "b"`,
		"1 2.5 up 4:5x x y":  `field "s"`,
		"1 2.5 up 4:50":      `field "r": required value is missing`,
		"1 2.5 up 4:50 x -":  `field "n": required value is null`,
	}
	for in, want := range cases {
		_, err := Parse(def, []byte(in+"\n"), Options{})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}
	empty := load(t, `
format: 1
command: t
variant: v
parse:
  type: kv
fields:
  a: {required: true}
`)
	_, err = Parse(empty, []byte("a=\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "required value is empty") {
		t.Errorf("required empty: %v", err)
	}
}

func TestLimitsAndEncoding(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: regex, pattern: '(?P<a>.*)'}\n")
	_, err := Parse(def, []byte("abc"), Options{MaxInputSize: 2})
	if !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("max input: %v", err)
	}
	_, err = Parse(def, []byte("abcdef\n"), Options{MaxLineLength: 3})
	if !errors.Is(err, ErrLineTooLong) {
		t.Errorf("max line: %v", err)
	}
	_, err = Parse(def, []byte{0xff, 0xfe}, Options{})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Errorf("invalid utf8: %v", err)
	}
	got, err := Parse(def, []byte("\xEF\xBB\xBFa\r\nb\r\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"a":"a"},{"a":"b"}]` {
		t.Errorf("BOM/CRLF: %v %v", mustJSON(t, got), err)
	}
	// A command run with -z or --zero ends its records with NUL instead
	// of a newline. Read line by line, that is one line holding every
	// record, and the last field of the first would take the rest.
	zero := []byte("a.txt\x00b.txt\x00")
	if _, err := Parse(def, zero, Options{}); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Errorf("NUL in a line: %v", err)
	}
	err = Stream(def, bytes.NewReader(zero), Options{}, func(any) error { return nil }, nil)
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Errorf("NUL in a streamed line: %v", err)
	}
	// A format whose records end with NUL reads the same text.
	nul := load(t, "format: 1\ncommand: t\nvariant: v\ninput: {record_separator: nul}\nparse: {type: regex, pattern: '(?P<a>.*)'}\n")
	if got, err := Parse(nul, zero, Options{}); err != nil || mustJSON(t, got) != `[{"a":"a.txt"},{"a":"b.txt"}]` {
		t.Errorf("NUL-separated: %v %v", mustJSON(t, got), err)
	}
}

func TestParseErrorFormatting(t *testing.T) {
	t.Parallel()
	e := &ParseError{Definition: "d/v", Line: 3, Field: "f", Msg: "bad", Cause: errors.New("cause")}
	if e.Error() != `d/v: line 3: field "f": bad: cause` {
		t.Error(e.Error())
	}
	if (&ParseError{Msg: "m"}).Error() != "m" {
		t.Error("minimal message")
	}
	if e.Unwrap() == nil {
		t.Error("unwrap")
	}
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: kv}\n")
	def.Parse.Type = "bogus"
	if _, err := Parse(def, []byte("a=b\n"), Options{}); err == nil {
		t.Error("unsupported type should error")
	}
}

func TestSplitFieldsN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s    string
		n    int
		want []string
	}{
		{"a b c", 0, []string{"a", "b", "c"}},
		{"a b c", 2, []string{"a", "b c"}},
		{"  a   b  c  ", 2, []string{"a", "b  c"}},
		{"a", 3, []string{"a"}},
		{"", 3, nil},
		{"a\tb", 1, []string{"a\tb"}},
	}
	for _, tt := range tests {
		if diff := diff(tt.want, splitFieldsN(tt.s, tt.n)); diff != "" {
			t.Errorf("splitFieldsN(%q,%d): %s", tt.s, tt.n, diff)
		}
	}
}

func TestAlignedCells(t *testing.T) {
	t.Parallel()
	cols := []column{{"a", 0}, {"b", 6}, {"c", 12}}
	tests := []struct {
		row  string
		want []raw
	}{
		{"x     y     z", []raw{some("x"), some("y"), some("z")}},
		{"x           z", []raw{some("x"), {}, some("z")}},
		{"x    12345  z", []raw{some("x"), some("12345"), some("z")}},
		{"x", []raw{some("x"), {}, {}}},
		{"", []raw{{}, {}, {}}},
		// A value wider than its column pushes the rest of the row right.
		{"abcdefgh y   z", []raw{some("abcdefgh"), some("y"), some("z")}},
		// A row that is not ASCII is cut by display column, not by byte.
		{"é     ü     z", []raw{some("é"), some("ü"), some("z")}},
		{"日本  y     z", []raw{some("日本"), some("y"), some("z")}},
	}
	for _, tt := range tests {
		got, bad := alignedCells(tt.row, cols, nil)
		if !slices.Equal(tt.want, got) || bad != nil {
			t.Errorf("alignedCells(%q) = %+v, refused %+v; want %+v", tt.row, got, bad, tt.want)
		}
	}
	for _, tt := range []struct {
		row    string
		col    int
		gutter bool
	}{
		// A value that runs past the start of the next column and leaves
		// it empty says nothing about whether that column was empty or its
		// header word belongs to this one ("CONTAINER ID").
		{"abcdefghijklmn", 1, false},
		{"abcdefgh     z", 1, false},
		{"x    abcdefghij", 2, false},
		// A cut inside a value with a space in it leaves the gap between
		// two columns inside a cell: "4 27s" is right-aligned under b.
		{"x  4 27s    z", 0, true},
		{"x\ty   z", 0, true},
	} {
		if _, bad := alignedCells(tt.row, cols, nil); bad == nil || bad.col != tt.col || bad.gutter != tt.gutter {
			t.Errorf("alignedCells(%q) = %+v, want column %d refused (gutter %v)", tt.row, bad, tt.col, tt.gutter)
		}
	}
	// The last column runs to the end of the line and may hold anything.
	if got, bad := alignedCells("x     y     z  z", cols, nil); bad != nil || got[2] != some("z  z") {
		t.Errorf("last column: %v %+v", got, bad)
	}
}

// Two outputs of one command in a row are two tables, the second opening
// with the same header. That line is a header, not a row: reading it as
// one wrote {"image":"IMAGE","id":"ID"} at exit 0. It is read as the
// header again, so an aligned table cut to other widths the second time
// is cut where its own header says.
func TestARepeatedHeaderStartsAnotherTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, def, in, want string
	}{
		{"whitespace", "parse: {type: table}\n",
			"A B\n1 2\nA B\n3 4\n",
			`[{"a":"1","b":"2"},{"a":"3","b":"4"}]`},
		{"aligned, other widths", "parse: {type: table, split: aligned}\n",
			"NAME  SIZE\nsda   20G\nNAME        SIZE\nnvme0n1     1T\n",
			`[{"name":"sda","size":"20G"},{"name":"nvme0n1","size":"1T"}]`},
		{"delimiter", "parse: {type: table, split: delimiter, delimiter: ':'}\n",
			"a:b\n1:2\na:b\n3:4\n",
			`[{"a":"1","b":"2"},{"a":"3","b":"4"}]`},
		// A csv file is data, so a row that holds the header's values is a
		// row unless the definition says the header is printed again.
		{"csv", "parse: {type: csv}\n",
			"name,count\nx,1\nname,count\ny,2\n",
			`[{"name":"x","count":"1"},{"name":"name","count":"count"},{"name":"y","count":"2"}]`},
		{"box", "parse: {type: table, split: box}\n",
			"+----+\n| id |\n+----+\n| 1  |\n+----+\n+----+\n| id |\n+----+\n| 2  |\n+----+\n",
			`[{"id":"1"},{"id":"2"}]`},
		// With no header line there is nothing to repeat, and a row that
		// happens to read like the column names is a row.
		{"no header", "parse: {type: table, header: {none: true, columns: [a, b]}}\n",
			"a b\n1 2\n",
			`[{"a":"a","b":"b"},{"a":"1","b":"2"}]`},
	}
	for _, tc := range cases {
		def := load(t, "format: 1\ncommand: t\nvariant: v\n"+tc.def)
		got, err := Parse(def, []byte(tc.in), Options{})
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		var buf bytes.Buffer
		if err := jsonutil.Encode(&buf, got, false); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(buf.String()) != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, buf.String(), tc.want)
		}
		var streamed []any
		if err := Stream(def, strings.NewReader(tc.in), Options{}, func(v any) error {
			streamed = append(streamed, v)
			return nil
		}, func(pe *ParseError) error { return pe }); err != nil {
			t.Errorf("%s: stream: %v", tc.name, err)
			continue
		}
		buf.Reset()
		if err := jsonutil.Encode(&buf, streamed, false); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(buf.String()) != tc.want {
			t.Errorf("%s: stream got %s, want %s", tc.name, buf.String(), tc.want)
		}
	}
}

// A drawn table whose only rules are its frame has no line that says
// where the header ends: read as a header over two lines, its rows were
// gone and the answer was [] at exit 0. psql with a border and no
// headings prints this.
func TestABoxWithNoRuleUnderItsHeaderIsRefused(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: box}\n")
	for _, in := range []string{
		"+---+-------+\n| 1 | alpha |\n| 2 | beta  |\n+---+-------+\n",
		"+---+-------+\n+---+-------+\n| 1 | alpha |\n| 2 | beta  |\n+---+-------+\n",
	} {
		if v, err := Parse(def, []byte(in), Options{}); err == nil || !strings.Contains(err.Error(), "no rule under its header") {
			t.Errorf("%q: %v, %v", in, v, err)
		}
		err := Stream(def, strings.NewReader(in), Options{}, func(any) error { return nil }, func(pe *ParseError) error { return pe })
		if err == nil || !strings.Contains(err.Error(), "no rule under its header") {
			t.Errorf("stream %q: %v", in, err)
		}
	}
	// One line under the frame is a header over no rows.
	if v, err := Parse(def, []byte("+----+\n| id |\n+----+\n"), Options{}); err != nil {
		t.Errorf("a header alone: %v, %v", v, err)
	}
}

// systemctl list-timers printed through the shape definition: LEFT is
// right-aligned and "4min 27s" starts before its header, at a space, so
// the cut left "... JST  4min" under NEXT at exit 0.
func TestAlignedRefusesACellThatHoldsAColumnGap(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned}\n")
	in := "NEXT                             LEFT LAST\n" +
		"Fri 2026-09-11 06:55:28 JST  4min 27s Fri 2026-09-11 06:50:28 JST\n"
	_, err := Parse(def, []byte(in), Options{})
	if err == nil || !strings.Contains(err.Error(), `line 2: field "next": "Fri 2026-09-11 06:55:28 JST  4min" holds the gap`) {
		t.Fatalf("err = %v", err)
	}
}

// docker ps printed through the shape definition: "CONTAINER ID" is one
// column under two header words, and the id runs past where the second
// one starts. That used to be a row with "id": null at exit 0.
func TestAlignedRefusesAValueThatRunsIntoAnEmptyColumn(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned}\n")
	in := "CONTAINER ID   IMAGE    NAMES\n43d84ed8db55   busybox  web\n"
	_, err := Parse(def, []byte(in), Options{})
	if err == nil || !strings.Contains(err.Error(), `line 2: field "id": `) {
		t.Fatalf("err = %v", err)
	}
	var got []any
	err = Stream(def, strings.NewReader(in), Options{}, func(v any) error {
		got = append(got, v)
		return nil
	}, func(pe *ParseError) error { return pe })
	if err == nil || len(got) != 0 {
		t.Errorf("stream: err = %v, records = %v", err, got)
	}
}

// A column named in header.columns by the name its header words derive
// ("CONTAINER ID" is container_id, "H/W path" is h_w_path) starts at the
// first of those words, so the table the test above refuses reads when
// the definition names that column. Words a gap apart are two columns
// even when their joined name is the declared one.
func TestAlignedColumnNamedByTwoHeaderWords(t *testing.T) {
	t.Parallel()
	docker := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned, header: {columns: [container_id, image, names]}}\n")
	in := "CONTAINER ID   IMAGE    NAMES\n43d84ed8db55   busybox  web\n"
	want := `[{"container_id":"43d84ed8db55","image":"busybox","names":"web"}]`
	got, err := Parse(docker, []byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	var streamed []any
	err = Stream(docker, strings.NewReader(in), Options{}, func(v any) error {
		streamed = append(streamed, v)
		return nil
	}, func(pe *ParseError) error { return pe })
	if err != nil {
		t.Fatal(err)
	}
	if diff := diff(want, mustJSON(t, streamed)); diff != "" {
		t.Errorf("stream: %s", diff)
	}

	lshw := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned, header: {columns: [h_w_path, device, class, description]}}\n")
	in = "H/W path        Device      Class      Description\n" +
		"                            system     Computer\n" +
		"/0/100/2.1/0    eno1        network    RTL8125 2.5GbE Controller\n"
	got, err = Parse(lshw, []byte(in), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want = `[{"h_w_path":null,"device":null,"class":"system","description":"Computer"},` +
		`{"h_w_path":"/0/100/2.1/0","device":"eno1","class":"network","description":"RTL8125 2.5GbE Controller"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}

	gap := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned, header: {columns: [a_b, c]}}\n")
	got, err = Parse(gap, []byte("A  B  C\n1  2  3\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := diff(`[{"a_b":"1","c":"2  3"}]`, mustJSON(t, got)); diff != "" {
		t.Errorf("words a gap apart: %s", diff)
	}
}

// A `free`-shaped definition read against a header that begins at the
// left edge. The unlabelled column is cut from the start of the line to
// where the first header word begins, so there it is empty and every
// value moves one column left. That used to be exit 0.
func TestAlignedRefusesALeadingLabelWithNoRoom(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned, header: {leading_label: kind}}\n")
	in := "total used\nMem: 10 5\n"
	_, err := Parse(def, []byte(in), Options{})
	if err == nil || !strings.Contains(err.Error(), `line 1: the header begins at the left edge, so the unlabelled column "kind" has no width`) {
		t.Fatalf("err = %v", err)
	}
}

// A parse retains one value per cell, so a wide header over many short
// rows retains far more than the input holds: 256 columns over rows of
// one letter is some thirteen thousand times the input on the heap. The
// input limit does not see it, since the input is small. What is bounded
// is what the reading produces.
func TestParseRefusesMoreValuesThanADocumentHolds(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: table, split: aligned}\n")
	var b strings.Builder
	for i := range 8 {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "c%d", i)
	}
	b.WriteByte('\n')
	for range 100 {
		b.WriteString("x\n")
	}
	in := b.String()
	// 100 rows of 8 cells are 800 values.
	if _, err := Parse(def, []byte(in), Options{MaxValues: 800}); err != nil {
		t.Fatalf("800 values within a limit of 800: %v", err)
	}
	_, err := Parse(def, []byte(in), Options{MaxValues: 799})
	if err == nil || !errors.Is(err, ErrTooManyValues) || !strings.Contains(err.Error(), "more than 799 values") {
		t.Fatalf("err = %v", err)
	}
	// A stream keeps one record at a time, so the limit is a record's:
	// the same input streams whole under a limit its rows fit in, and a
	// record that does not fit is the one refused.
	var got []any
	err = Stream(def, strings.NewReader(in), Options{MaxValues: 8}, func(v any) error {
		got = append(got, v)
		return nil
	}, nil)
	if err != nil || len(got) != 100 {
		t.Fatalf("stream under a record's worth: err = %v, records = %d", err, len(got))
	}
	got = nil
	err = Stream(def, strings.NewReader(in), Options{MaxValues: 7}, func(v any) error {
		got = append(got, v)
		return nil
	}, nil)
	if err == nil || !errors.Is(err, ErrTooManyValues) || len(got) != 0 {
		t.Fatalf("stream over a record's worth: err = %v, records = %d", err, len(got))
	}
}

func FuzzParse(f *testing.F) {
	defs := []string{dfDef,
		"format: 1\ncommand: t\nvariant: a\nparse: {type: table, split: aligned}\nfields: {size: {type: int}}\n",
		"format: 1\ncommand: t\nvariant: k\nparse: {type: kv, as: map}\nfields: {n: {type: int}}\n",
		"format: 1\ncommand: t\nvariant: r\nparse: {type: regex, each: input, pattern: '(?P<a>\\d+)(?P<b>x)?'}\nfields: {a: {type: int}, b: {when_missing: omit}}\n",
	}
	f.Add(0, []byte(dfInput))
	f.Add(1, []byte("NAME SIZE\nsda 20G\n"))
	f.Add(2, []byte("n=1\n"))
	f.Add(3, []byte("12x"))
	f.Add(0, []byte("\x1b[31m\xef\xbb\xbfFilesystem 1K-blocks Used Available Use% Mounted on\x1b[0m\r\n/dev/sda1 1 2 3 4% /\r\n"))
	f.Add(0, []byte("\n\n\nFilesystem 1K-blocks Used Available Use% Mounted on\n/dev/sda1 1 2 3 4% /\n\n"))
	f.Add(1, []byte("NAME  SIZE\nsda   20G\nNAME  SIZE\nsdb   1T\n"))
	f.Fuzz(func(t *testing.T, which int, input []byte) {
		if which < 0 {
			which = -which
		}
		src := defs[which%len(defs)]
		d, err := definition.Load([]byte(src), "fuzz")
		if err != nil {
			t.Fatal(err)
		}
		v, acct, err := ParseAccounted(d, input, Options{MaxInputSize: 1 << 20, MaxValues: 1 << 16})
		if err != nil {
			return
		}
		if _, err := jsonutil.Marshal(v); err != nil {
			t.Fatalf("result not encodable: %v", err)
		}
		// A reading that succeeded has put every line somewhere.
		accounted := acct.Read + acct.Folded + acct.Blank
		for _, ig := range acct.Ignored {
			accounted += ig.Lines
		}
		if accounted != acct.Lines {
			t.Fatalf("the account covers %d of %d lines: %+v", accounted, acct.Lines, acct)
		}
		sameAsStream(t, d, input, v)
	})
}

func TestParseRegexSeveralPatterns(t *testing.T) {
	t.Parallel()
	// The shape of the line decides how it is read: only a mode string
	// starting with "l" makes " -> " a link separator.
	def := load(t, `
format: 1
command: ls
variant: long
parse:
  type: regex
  patterns:
    - '^(?P<flags>l\S+)\s+(?P<filename>.+?) -> (?P<link_to>.+)$'
    - '^(?P<flags>\S+)\s+(?P<filename>.+)$'
fields:
  link_to: {when_missing: omit}
`)
	input := "lrwxrwxrwx bin -> usr/bin\n-rw-r--r-- a -> b\nlrwxrwxrwx a -> b -> c\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"flags":"lrwxrwxrwx","filename":"bin","link_to":"usr/bin"},` +
		`{"flags":"-rw-r--r--","filename":"a -> b"},` +
		`{"flags":"lrwxrwxrwx","filename":"a","link_to":"b -> c"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// A line that fits none of the alternatives names them all.
	_, err = Parse(def, []byte("\n x\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "any of the 2 patterns") {
		t.Errorf("mismatch message: %v", err)
	}
	whole := load(t, `
format: 1
command: x
variant: v
parse:
  type: regex
  each: input
  patterns: ['^only-this (?P<a>\S+)$', '^(?P<b>.+)$']
`)
	got, err = Parse(whole, []byte("something else\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"b":"something else"}` {
		t.Errorf("each input with patterns: %v %v", mustJSON(t, got), err)
	}
}

func TestParsePatternValues(t *testing.T) {
	t.Parallel()
	// Each alternative states the kind of line it reads; the stated
	// values come before the groups, and the order of the input stays.
	def := load(t, `
format: 1
command: x
variant: v
parse:
  type: regex
  patterns:
    - pattern: '^(?P<name>\w+)=(?P<value>.*)$'
      values: {kind: environment}
    - pattern: '^(?P<minute>\d+) (?P<command>.+)$'
      values: {kind: job, a_first: "yes"}
`)
	input := "SHELL=/bin/sh\n5 run it\nMAILTO=\n"
	want := `[{"kind":"environment","name":"SHELL","value":"/bin/sh"},` +
		`{"a_first":"yes","kind":"job","minute":"5","command":"run it"},` +
		`{"kind":"environment","name":"MAILTO","value":""}]`
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	var streamed []string
	err = Stream(def, strings.NewReader(input), Options{}, func(v any) error {
		streamed = append(streamed, mustJSON(t, v))
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := "[" + strings.Join(streamed, ",") + "]"; got != want {
		t.Errorf("stream = %s, want %s", got, want)
	}
}

func TestStringFieldRegex(t *testing.T) {
	t.Parallel()
	// The one group of a string field's regex is the value; the rest of
	// the match is taken off it.
	def := load(t, `
format: 1
command: x
variant: v
parse:
  type: regex
  pattern: '^(?P<name>\S+) (?P<note>.*)$'
fields:
  name: {regex: '(?:[|`+"`"+`]-)?(?P<name>.+)'}
  note: {regex: '(?:\((?P<note>[^)]*)\))?', when_missing: omit}
`)
	got, err := Parse(def, []byte("|-sda1 (boot)\nsda x\n"), Options{})
	if err == nil {
		t.Fatalf("a value outside the shape was read: %s", mustJSON(t, got))
	}
	if !strings.Contains(err.Error(), `value "x" does not have the shape`) {
		t.Errorf("error = %v", err)
	}
	got, err = Parse(def, []byte("|-sda1 (boot)\n`-sda2 \nsda \n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// A group that takes no part leaves the value missing, and
	// when_missing decides what that becomes.
	want := `[{"name":"sda1","note":"boot"},{"name":"sda2"},{"name":"sda"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// Raw mode leaves the field rules out, the regex with them.
	got, err = Parse(def, []byte("|-sda1 (boot)\n"), Options{Raw: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff := diff(`[{"name":"|-sda1","note":"(boot)"}]`, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
}

func TestStringFieldUnescape(t *testing.T) {
	t.Parallel()
	// The escapes are undone only on a line whose marker group took part,
	// so an unmarked backslash stays what it is.
	def := load(t, `
format: 1
command: x
variant: v
parse:
  type: regex
  pattern: '^(?P<escaped>\\)?(?P<sum>[0-9a-f]{4})  (?P<file>.+)$'
fields:
  escaped: {type: bool, true_values: ["\\"], when_missing: omit}
  file: {unescape: {when: escaped, sequences: {'\\': '\', '\n': "\n", '\r': "\r"}}}
`)
	input := "\\abcd  back\\\\slash\\nnl\\r\n" +
		"abcd  raw\\name\n" +
		"\\abcd  lit\\\\n\n"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"escaped":true,"sum":"abcd","file":"back\\slash\nnl\r"},` +
		`{"sum":"abcd","file":"raw\\name"},` +
		`{"escaped":true,"sum":"abcd","file":"lit\\n"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// An escape the format does not write is not guessed at, and neither
	// is a backslash at the end.
	for _, bad := range []string{"\\abcd  tab\\tname\n", "\\abcd  end\\\n"} {
		got, err := Parse(def, []byte(bad), Options{})
		if err == nil || !strings.Contains(err.Error(), "holds an escape the format does not write") {
			t.Errorf("%q: got %s, %v", bad, mustJSON(t, got), err)
		}
	}
	// Without when, every value is decoded.
	always := load(t, `
format: 1
command: x
variant: v
parse:
  type: regex
  pattern: '^(?P<name>.+)$'
fields:
  name: {unescape: {sequences: {'\ ': ' ', '\\': '\'}}}
`)
	got, err = Parse(always, []byte("a\\ b\\\\c\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := diff(`[{"name":"a b\\c"}]`, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
}

func TestParseKVKeepsWhitespaceWhenAsked(t *testing.T) {
	t.Parallel()
	trimmed := load(t, "format: 1\ncommand: x\nvariant: v\nparse: {type: kv}\n")
	verbatim := load(t, "format: 1\ncommand: env\nvariant: posix\nparse: {type: kv, trim: false}\n")
	input := "A=  padded  \nB=x\nC=\n"
	got, err := Parse(trimmed, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustJSON(t, got), `{"name":"A","value":"padded"}`) {
		t.Errorf("trim default: %s", mustJSON(t, got))
	}
	got, err = Parse(verbatim, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"A","value":"  padded  "},{"name":"B","value":"x"},{"name":"C","value":""}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// A field rule must not quietly undo the preservation either.
	typed := load(t, `
format: 1
command: env
variant: posix
parse: {type: kv, trim: false, as: map}
fields:
  PADDED: {}
  NUM: {type: int}
  DASH: {null_if: ["-"]}
`)
	got, err = Parse(typed, []byte("PADDED=  keep me  \nNUM=  42  \nDASH=  -  \n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"PADDED":"  keep me  ","NUM":42,"DASH":null}`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// Required still rejects a value that is only whitespace.
	req := load(t, "format: 1\ncommand: env\nvariant: posix\nparse: {type: kv, trim: false, as: map}\nfields: {A: {required: true}}\n")
	if _, err := Parse(req, []byte("A=   \n"), Options{}); err == nil ||
		!strings.Contains(err.Error(), "required value is empty") {
		t.Errorf("required with whitespace: %v", err)
	}
}

func TestParseNULSeparatedRecords(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: env
variant: null-separated
input:
  record_separator: nul
parse: {type: kv, trim: false}
`)
	// The NUL form is the one that survives a value containing a newline.
	input := "A=one\nstill A\x00B=two\x00PADDED=  x  \x00"
	got, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"A","value":"one\nstill A"},{"name":"B","value":"two"},{"name":"PADDED","value":"  x  "}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// The same bytes read as lines cannot tell the two apart, which is
	// why the NUL variant exists.
	lines := load(t, "format: 1\ncommand: env\nvariant: posix\nparse: {type: kv, trim: false}\n")
	got, err = Parse(lines, []byte("A=one\nB=two\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"name":"A","value":"one"},{"name":"B","value":"two"}]` {
		t.Errorf("newline form: %v %v", mustJSON(t, got), err)
	}
	// A record longer than the limit is reported as such.
	if _, err := Parse(def, []byte("A=xxxxxxxxxx\x00"), Options{MaxLineLength: 4}); !errors.Is(err, ErrLineTooLong) {
		t.Errorf("record limit: %v", err)
	}
}

func TestStringFieldsKeepWhitespace(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: x
variant: v
parse:
  type: regex
  pattern: '^\[(?P<padded>.*)\]\[(?P<trimmed>.*)\]$'
fields:
  trimmed: {trim_suffix: "!"}
`)
	got, err := Parse(def, []byte("[  a  ][  b!  ]\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Without an explicit trim rule the captured text is untouched; with
	// one, the surrounding space goes with it.
	want := `[{"padded":"  a  ","trimmed":"b"}]`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
}

func TestParseKVUnquote(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: os-release\nvariant: linux\nparse: {type: kv, as: map, unquote: true}\n")
	got, err := Parse(def, []byte(`NAME="Ubuntu"
VERSION="26.04 LTS (Resolute Raccoon)"
ID=ubuntu
SINGLE='quoted'
UNBALANCED="left
EMPTY=""
`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"NAME":"Ubuntu","VERSION":"26.04 LTS (Resolute Raccoon)","ID":"ubuntu","SINGLE":"quoted","UNBALANCED":"\"left","EMPTY":""}`
	if diff := diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// Without the option the quotes are content.
	plain := load(t, "format: 1\ncommand: x\nvariant: v\nparse: {type: kv, as: map}\n")
	got, err = Parse(plain, []byte("NAME=\"Ubuntu\"\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"NAME":"\"Ubuntu\""}` {
		t.Errorf("without unquote: %v %v", mustJSON(t, got), err)
	}
}

// Raw skips the field rules. What comes out is what the definition
// extracted, which is what makes a definition debuggable: a value that
// converts wrongly and a value that was cut from the wrong place look the
// same once they are typed.
func TestParseRawSkipsTheFieldRules(t *testing.T) {
	t.Parallel()
	const src = `format: 1
command: t
variant: v
parse: {type: table, header: {none: true, columns: [n, pct, flag, when]}}
fields:
  n: {type: int}
  pct: {trim_suffix: "%", type: int}
  flag: {type: bool, null_if: ["-"]}
  when: {type: time, layout: "2006-01-02"}
`
	def := load(t, src)
	input := "12 92% - 2024-05-06\n"

	typed, err := Parse(def, []byte(input), Options{})
	if err != nil {
		t.Fatalf("typed: %v", err)
	}
	if got := mustJSON(t, typed); got != `[{"n":12,"pct":92,"flag":null,"when":"2024-05-06T00:00:00Z"}]` {
		t.Errorf("typed = %s", got)
	}

	raw, err := Parse(def, []byte(input), Options{Raw: true})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	// Every value is the text that was extracted: no conversion, no
	// trim_suffix, no null_if.
	if got := mustJSON(t, raw); got != `[{"n":"12","pct":"92%","flag":"-","when":"2024-05-06"}]` {
		t.Errorf("raw = %s", got)
	}
}

// A shape that changed with the values in it would defeat the point, so
// required and when_missing are left out with the rest of the field
// rules and a group that did not take part is null.
func TestParseRawKeepsTheShape(t *testing.T) {
	t.Parallel()
	const src = `format: 1
command: t
variant: v
parse: {type: regex, pattern: '^(?P<a>\w+)(?: (?P<b>\w+))?$'}
fields:
  a: {required: true, type: int}
  b: {when_missing: omit}
`
	def := load(t, src)
	raw, err := Parse(def, []byte("xyz\n"), Options{Raw: true})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if got := mustJSON(t, raw); got != `[{"a":"xyz","b":null}]` {
		t.Errorf("raw = %s", got)
	}
	// The same input typed is a failure, which is the difference raw mode
	// exists to show.
	if _, err := Parse(def, []byte("xyz\n"), Options{}); err == nil {
		t.Error("typed accepted a value it cannot convert")
	}
}

// An object field's sub-fields are read in the object's own match: an
// unescape rule whose when names a group of the object's regex is
// decided by that group, not by the groups of the line's pattern.
func TestNestedObjectUnescapeWhen(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: t
variant: v
parse: {type: regex, pattern: '(?P<x>.*)'}
fields:
  x:
    type: object
    regex: '(?P<flag>!)?(?P<name>.*)'
    fields:
      name: {unescape: {when: flag, sequences: {'\n': "\n"}}}
`)
	got, err := Parse(def, []byte("!a\\nb\nc\\nd\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"x":{"flag":"!","name":"a\nb"}},{"x":{"flag":null,"name":"c\\nd"}}]`
	if s := mustJSON(t, got); s != want {
		t.Errorf("got %s, want %s", s, want)
	}
}

// The escape sequences come off first, then the carriage return, then
// the byte order mark, in a whole document as in a stream, so a byte
// order mark behind a colour code and a carriage return inside a
// sequence read the same both ways.
func TestRecordPreparationIsTheSameBothWays(t *testing.T) {
	t.Parallel()
	def := load(t, "format: 1\ncommand: t\nvariant: v\nparse: {type: regex, pattern: '(?P<x>.*)'}\n")
	inputs := []string{
		"\x1b[31m\xef\xbb\xbfabc\n",
		"\xef\xbb\xbf\x1b[31mabc\x1b[0m\r\n",
		"abc\x1b[31m\r\n\x1b[0mdef\r\n",
		"\x1b[31mabc\x1b[K\r\n",
		"a\r\nb\rc\n",
	}
	for _, in := range inputs {
		whole, err := Parse(def, []byte(in), Options{})
		if err != nil {
			t.Errorf("%q: whole: %v", in, err)
			continue
		}
		var streamed []any
		if err := Stream(def, strings.NewReader(in), Options{}, func(v any) error {
			streamed = append(streamed, v)
			return nil
		}, nil); err != nil {
			t.Errorf("%q: stream: %v", in, err)
			continue
		}
		if a, b := mustJSON(t, whole), mustJSON(t, streamed); a != b {
			t.Errorf("%q: whole %s, stream %s", in, a, b)
		}
		if s := mustJSON(t, whole); strings.Contains(s, "\\ufeff") || strings.Contains(s, "\\u001b") {
			t.Errorf("%q: %s keeps a mark or an escape", in, s)
		}
	}
}

// A record may be one value rather than named regions: a report whose
// block is one labelled list, or one expression over the whole block,
// has nothing to name the regions of, and the record is then the object
// that parser yields.
func TestParseRecordsWithOneParser(t *testing.T) {
	t.Parallel()
	def := load(t, `
format: 1
command: x
variant: blocks
parse:
  type: records
  start: '^name: '
  record:
    parse:
      type: kv
      separator: ':'
      as: map
    fields:
      size: {type: int}
`)
	got, err := Parse(def, []byte("name: a\nsize: 1\nname: b\nsize: 2\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"name":"a","size":1},{"name":"b","size":2}]`
	if mustJSON(t, got) != want {
		t.Error(mustJSON(t, got))
	}

	// The stream reads the same records in the same order.
	var stream strings.Builder
	if err := Stream(def, strings.NewReader("name: a\nsize: 1\nname: b\nsize: 2\n"), Options{}, func(v any) error {
		stream.WriteString(mustJSON(t, v))
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	if stream.String() != `{"name":"a","size":1}{"name":"b","size":2}` {
		t.Error(stream.String())
	}
}

// diff reports how got differs from want, or nothing when they are the
// same. Strings are shown as they are, since most of them are JSON.
func diff(want, got any) string {
	if reflect.DeepEqual(want, got) {
		return ""
	}
	if w, ok := want.(string); ok {
		return fmt.Sprintf("\n want %s\n  got %v", w, got)
	}
	return fmt.Sprintf("\n want %#v\n  got %#v", want, got)
}
