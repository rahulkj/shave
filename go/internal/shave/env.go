package shave

import (
	"fmt"
	"io"
	"os"
)

// Env holds runtime context for a command invocation.
type Env struct {
	Root string
	Out  io.Writer
	buf  []string
	Err  io.Writer
}

func newEnv() *Env {
	cwd, _ := os.Getwd()
	return &Env{Root: cwd, Out: os.Stdout, Err: os.Stderr}
}

func (e *Env) Printf(f string, a ...interface{}) {
	fmt.Fprintf(e.Out, f, a...)
}

func (e *Env) Flush() {
	for _, s := range e.buf {
		fmt.Fprintln(e.Out, s)
	}
	e.buf = nil
}

func terse() bool {
	return os.Getenv("SHAVE_TERSE") == "1" || os.Getenv("SHAVE_TERSE") == "true"
}
