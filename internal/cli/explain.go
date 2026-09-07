package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nao1215/jsonize/pkg/selector"
)

// explain writes what the selector decided and what it decided it on.
// The choice is the one thing a converter has to get right, and until
// now a successful run said nothing about it: the JSON looked the same
// whichever definition produced it. Everything here goes to standard
// error, so stdout stays a JSON document or nothing, and the exit status
// is untouched.
//
// No timestamp is written. The output is meant to be compared between
// runs and pasted into a report, and a clock reading would make two
// identical explanations differ.
func (a *app) explain(res *selector.Result) {
	if res == nil {
		return
	}
	a.errorf("%s from %s", res.Entry.Def.ID(), res.Entry.Def.Origin)
	if len(res.Matched) > 0 {
		a.errorf("matched: %s", strings.Join(res.Matched, ", "))
	}
	a.explainRejections(res.Rejections)
}

// explainRejections lists the definitions that were left out. A search of
// the whole registry passes only the near misses here, since every other
// definition was ruled out by the first expression of its signature and
// saying so three hundred times explains nothing.
func (a *app) explainRejections(rs []selector.Rejection) {
	if len(rs) == 0 {
		return
	}
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = fmt.Sprintf("%s (%s)", r.Entry.Def.ID(), r.Reason)
	}
	a.errorf("rejected: %s", strings.Join(parts, ", "))
}

// explainFailure reports a selection that produced no answer. The exit
// status and the message the user gets are unchanged; --explain adds the
// candidates behind them.
func (a *app) explainFailure(err error) {
	var (
		nm *selector.NoMatchError
		am *selector.AmbiguousError
	)
	switch {
	case errors.As(err, &nm):
		a.explainRejections(nm.Reported)
	case errors.As(err, &am):
		a.explainRejections(am.Reported)
	}
}

// explainCommand reports the command jz ran and the status it gave.
func (a *app) explainCommand(name string, args []string, exitCode int) {
	line := name
	if len(args) > 0 {
		line += " " + strings.Join(args, " ")
	}
	a.errorf("command: %s (exit %d)", line, exitCode)
}
