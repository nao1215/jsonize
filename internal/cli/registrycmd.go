package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nao1215/jsonize/internal/registry"
	"github.com/nao1215/jsonize/internal/remote"
)

const registryUsage = `Usage: jz registry <update|paths> [flags]

  update   download the official registry archive, verify its SHA-256,
           validate every definition and install it atomically
  paths    print the registry directories in precedence order

Flags (update):
`

func (a *app) cmdRegistry(args []string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == flagHelp {
		w := a.env.Stdout
		code := ExitOK
		if len(args) == 0 {
			w = a.env.Stderr
			code = ExitUsage
		}
		fmt.Fprint(w, registryUsage)
		fs := newFlagSet("registry update")
		a.bindUpdateFlags(fs)
		fs.SetOutput(w)
		fs.PrintDefaults()
		return code
	}
	switch args[0] {
	case "update":
		return a.cmdRegistryUpdate(args[1:])
	case "paths":
		return a.cmdRegistryPaths(args[1:])
	default:
		a.errorf("registry: unknown subcommand %q (expected update or paths)", args[0])
		return ExitUsage
	}
}

type updateFlags struct {
	source   string
	checksum string
	dir      string
	insecure bool
}

func (a *app) bindUpdateFlags(fs interface {
	StringVar(*string, string, string, string)
	BoolVar(*bool, string, bool, string)
}) *updateFlags {
	var f updateFlags
	fs.StringVar(&f.source, "source", remote.DefaultURL, "archive `URL` (https)")
	fs.StringVar(&f.checksum, "checksum", "", "checksum file `URL` (default: source + .sha256)")
	fs.StringVar(&f.dir, "dir", "", "install into this `directory` instead of the cache")
	fs.BoolVar(&f.insecure, "allow-http", false, "permit plain http:// sources (testing only)")
	return &f
}

func (a *app) cmdRegistryUpdate(args []string) int {
	fs := newFlagSet("registry update")
	f := a.bindUpdateFlags(fs)
	if code, done := a.parseFlags(fs, args, registryUsage); done {
		return code
	}
	if fs.NArg() != 0 {
		a.errorf("registry update takes no positional arguments")
		return ExitUsage
	}
	dest := f.dir
	if dest == "" {
		d, err := a.cacheRegistryDir()
		if err != nil {
			a.errorf("cannot locate the cache directory: %v", err)
			return ExitError
		}
		dest = d
	}
	report, err := remote.Update(a.env.Context, remote.Options{
		URL:           f.source,
		ChecksumURL:   f.checksum,
		Dest:          dest,
		Client:        a.env.HTTPClient,
		AllowInsecure: f.insecure,
		Validate:      validateDownloaded,
	})
	if err != nil {
		a.errorf("registry update failed: %v", err)
		if errors.Is(err, remote.ErrInsecureURL) {
			return ExitUsage
		}
		return ExitRegistry
	}
	verb := "installed"
	if report.Replaced {
		verb = "updated"
	}
	fmt.Fprintf(a.env.Stdout, "%s registry in %s (%d files, %d bytes, sha256 %s)\n", verb, dest, report.Files, report.Bytes, report.SHA256[:12])
	return ExitOK
}

// validateDownloaded refuses an archive whose definitions do not all load.
func validateDownloaded(dir string) error {
	reg, err := registry.Load(registry.Source{Name: "download", FS: os.DirFS(dir)})
	if err != nil {
		return err
	}
	if len(reg.Problems) > 0 {
		return errors.Join(reg.Problems...)
	}
	if reg.Len() == 0 {
		return errors.New("archive contains no parser definitions")
	}
	return nil
}

func (a *app) cmdRegistryPaths(args []string) int {
	fs := newFlagSet("registry paths")
	var rf registryFlags
	rf.bind(fs)
	if code, done := a.parseFlags(fs, args, "Usage: jz registry paths [flags]\n\nFlags:\n"); done {
		return code
	}
	srcs, err := a.sources(&rf)
	if err != nil {
		a.errorf("%v", err)
		return ExitRegistry
	}
	for i, s := range srcs {
		status := "present"
		loc := s.Name
		switch s.Name {
		case SourceEmbedded, a.env.Embedded.Name:
			loc = "(built into jz)"
		case SourceUser:
			loc, _ = a.userRegistryDir()
		case SourceCache:
			loc, _ = a.cacheRegistryDir()
		}
		if s.Name != SourceEmbedded && s.Name != a.env.Embedded.Name {
			if _, err := os.Stat(filepath.Join(loc, registry.ParsersDir)); err != nil {
				status = "absent"
			}
		}
		fmt.Fprintf(a.env.Stdout, "%d. %-15s %-8s %s\n", i+1, s.Name, status, loc)
	}
	return ExitOK
}
