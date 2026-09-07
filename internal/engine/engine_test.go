package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/nao1215/jsonize/internal/definition"
	"github.com/nao1215/jsonize/internal/jsonutil"
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
  size: {type: size}
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
	want := `[{"name":"sda","maj_min":"8:0","rm":false,"size":21474836480,"ro":false,"type":"disk","mountpoint":null},` +
		`{"name":"├─sda1","maj_min":"8:1","rm":false,"size":1073741824,"ro":false,"type":"part","mountpoint":"/boot"},` +
		`{"name":"├─centos-root","maj_min":"253:0","rm":false,"size":18253611008,"ro":false,"type":"lvm","mountpoint":"/"},` +
		`{"name":"loop9","maj_min":"7:9","rm":false,"size":129394278,"ro":true,"type":"loop","mountpoint":"/snap/x y"}]`
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	_, err = Parse(derived, []byte("   \nx\n"), Options{})
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	got, err = Parse(def, []byte("uid=501(k) gid=20 groups=20(staff)\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	want = `{"uid":{"id":501,"name":"k"},"gid":{"id":20},"groups":[{"id":20,"name":"staff"}]}`
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	got, err = Parse(m, []byte("name : jsonize\ncount: 3\nflag: yes\nname: last wins\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `{"name":"last wins","count":3,"flag":true}` {
		t.Error(mustJSON(t, got))
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	_, err = Parse(def, []byte("bogus\nUSER TTY\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), `part "uptime"`) {
		t.Errorf("expected part error, got %v", err)
	}
}

func TestSelect(t *testing.T) {
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
	got, err := Parse(def, []byte("junk\nBEGIN\nskipme\na\nb\nc\nEND\nafter\n"), Options{})
	if err != nil || mustJSON(t, got) != `[{"v":"a"},{"v":"b"}]` {
		t.Errorf("select: %v %v", mustJSON(t, got), err)
	}
	got, err = Parse(def, []byte("no begin marker\n"), Options{})
	if err != nil || mustJSON(t, got) != `[]` {
		t.Errorf("missing after: %v %v", mustJSON(t, got), err)
	}
	got, err = Parse(def, []byte("BEGIN\nonly\n"), Options{})
	if err != nil || mustJSON(t, got) != `[]` {
		t.Errorf("skip beyond end: %v %v", mustJSON(t, got), err)
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
  s: {type: size, unit: decimal}
  r: {required: true}
  n: {null_if: ["-"], required: true}
  o: {when_missing: omit}
`)
	got, err := Parse(def, []byte("1 2.5 up 1K x y\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, got) != `[{"i":1,"f":2.5,"b":true,"s":1000,"r":"x","n":"y"}]` {
		t.Error(mustJSON(t, got))
	}
	cases := map[string]string{
		"x 2.5 up 1K x y":  `field "i": cannot convert "x" to int`,
		"1 x up 1K x y":    `field "f"`,
		"1 2.5 maybe 1K x": `field "b"`,
		"1 2.5 up 1Q x y":  `field "s"`,
		"1 2.5 up 1K":      `field "r": required value is missing`,
		"1 2.5 up 1K x -":  `field "n": required value is null`,
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
		if diff := cmp.Diff(tt.want, splitFieldsN(tt.s, tt.n)); diff != "" {
			t.Errorf("splitFieldsN(%q,%d): %s", tt.s, tt.n, diff)
		}
	}
}

func TestAlignedCells(t *testing.T) {
	t.Parallel()
	cols := []column{{"a", 0}, {"b", 6}, {"c", 12}}
	tests := []struct {
		row  string
		want []any
	}{
		{"x     y     z", []any{"x", "y", "z"}},
		{"x           z", []any{"x", nil, "z"}},
		{"x    12345  z", []any{"x", "12345", "z"}},
		{"x", []any{"x", nil, nil}},
		{"", []any{nil, nil, nil}},
		{"abcdefghijklmn", []any{"abcdefghijklmn", nil, nil}},
	}
	for _, tt := range tests {
		if diff := cmp.Diff(tt.want, alignedCells(tt.row, cols)); diff != "" {
			t.Errorf("alignedCells(%q): %s", tt.row, diff)
		}
	}
}

func FuzzParse(f *testing.F) {
	defs := []string{dfDef,
		"format: 1\ncommand: t\nvariant: a\nparse: {type: table, split: aligned}\nfields: {size: {type: size}}\n",
		"format: 1\ncommand: t\nvariant: k\nparse: {type: kv, as: map}\nfields: {n: {type: int}}\n",
		"format: 1\ncommand: t\nvariant: r\nparse: {type: regex, each: input, pattern: '(?P<a>\\d+)(?P<b>x)?'}\nfields: {a: {type: int}, b: {when_missing: omit}}\n",
	}
	f.Add(0, []byte(dfInput))
	f.Add(1, []byte("NAME SIZE\nsda 20G\n"))
	f.Add(2, []byte("n=1\n"))
	f.Add(3, []byte("12x"))
	f.Fuzz(func(t *testing.T, which int, input []byte) {
		if which < 0 {
			which = -which
		}
		src := defs[which%len(defs)]
		d, err := definition.Load([]byte(src), "fuzz")
		if err != nil {
			t.Fatal(err)
		}
		v, err := Parse(d, input, Options{MaxInputSize: 1 << 20})
		if err != nil {
			return
		}
		if _, err := jsonutil.Marshal(v); err != nil {
			t.Fatalf("result not encodable: %v", err)
		}
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
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
	if diff := cmp.Diff(want, mustJSON(t, got)); diff != "" {
		t.Error(diff)
	}
	// Without the option the quotes are content.
	plain := load(t, "format: 1\ncommand: x\nvariant: v\nparse: {type: kv, as: map}\n")
	got, err = Parse(plain, []byte("NAME=\"Ubuntu\"\n"), Options{})
	if err != nil || mustJSON(t, got) != `{"NAME":"\"Ubuntu\""}` {
		t.Errorf("without unquote: %v %v", mustJSON(t, got), err)
	}
}
