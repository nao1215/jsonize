package selector_test

import (
	"fmt"
	"log"
	"os"

	"github.com/nao1215/jsonize/pkg/engine"
	"github.com/nao1215/jsonize/pkg/jsonutil"
	"github.com/nao1215/jsonize/pkg/registry"
	"github.com/nao1215/jsonize/pkg/selector"
	official "github.com/nao1215/jsonize/registry"
)

// The four packages jz itself is built from, in the order it uses them:
// registry loads the definitions, selector says which of them describes
// the text, engine reads it with that definition, and jsonutil writes the
// result with its keys in the order the definition names them.
func Example() {
	reg, err := registry.Load(registry.Source{Name: "embedded", FS: official.FS()})
	if err != nil {
		log.Fatal(err)
	}
	out := []byte("Filesystem     1K-blocks    Used Available Use% Mounted on\n" +
		"tmpfs            1000000    5000    995000   1% /run\n")

	// An empty context is what `COMMAND | jz` has: the text and nothing
	// else. Naming the command and the operating system narrows the
	// candidates the way `jz run` does.
	res, err := selector.Select(reg, selector.Context{Input: out})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Entry.Def.ID())

	v, err := engine.Parse(res.Entry.Def, out, engine.Options{})
	if err != nil {
		log.Fatal(err)
	}
	if err := jsonutil.Encode(os.Stdout, v, false); err != nil {
		log.Fatal(err)
	}
	// Output:
	// df/gnu
	// [{"filesystem":"tmpfs","1k_blocks":1000000,"used":5000,"available":995000,"use_percent":1,"mounted_on":"/run"}]
}
