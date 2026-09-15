package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"
)

// interrupts counts the interrupts the process receives for a while,
// one at a time, where a shell's trap merges two that arrive close
// together. It writes "ready" to standard error once it is listening,
// "interrupt N" there for each one, and "count=N" to standard output at
// the end, which is how a scenario sees whether one keypress was
// delivered once or twice.
//
//	e2ehelper interrupts -for D
func interrupts(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("interrupts", flag.ContinueOnError)
	wait := fs.Duration("for", 2*time.Second, "how long to count")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ch := make(chan os.Signal, 16)
	signal.Notify(ch, os.Interrupt)
	defer signal.Stop(ch)
	fmt.Fprintln(stderr, "ready")
	deadline := time.After(*wait)
	n := 0
	for {
		select {
		case <-ch:
			n++
			fmt.Fprintf(stderr, "interrupt %d\n", n)
		case <-deadline:
			_, err := fmt.Fprintf(stdout, "count=%d\n", n)
			return err
		}
	}
}
