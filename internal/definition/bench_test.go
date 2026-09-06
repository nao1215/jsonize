package definition

import "testing"

func BenchmarkLoadAndValidate(b *testing.B) {
	src := []byte(validTable)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load(src, "bench"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateNested(b *testing.B) {
	src := []byte(`
format: 1
command: id
variant: posix
detect: {os: [linux], signature: {all: ['^uid=']}}
parse:
  type: regex
  each: input
  pattern: '^uid=(?P<uid>\S+) gid=(?P<gid>\S+) groups=(?P<groups>\S+)$'
fields:
  uid: {type: object, regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$', fields: {id: {type: int}}}
  gid: {type: object, regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$', fields: {id: {type: int}}}
  groups: {type: array, split: ",", items: {type: object, regex: '^(?P<id>\d+)\((?P<name>[^)]*)\)$', fields: {id: {type: int}}}}
`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load(src, "bench"); err != nil {
			b.Fatal(err)
		}
	}
}
