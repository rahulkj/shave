package shave

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRedactLineRedactsSecrets(t *testing.T) {
	in := "password=hunter2 token: abc123"
	out := redactLine(in)
	if strings.Contains(out, "hunter2") || strings.Contains(out, "abc123") {
		t.Fatalf("secret not redacted: %s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("missing redaction marker: %s", out)
	}
}

func TestSanitizeKey(t *testing.T) {
	if got := sanitizeKey("a/b c"); got != "a_b_c" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeKey(""); got != "default" {
		t.Fatalf("got %q", got)
	}
}

func TestLogCmdDedupe(t *testing.T) {
	oldIn, oldOut := os.Stdin, os.Stdout
	r, w, _ := os.Pipe()
	rin, win, _ := os.Pipe()
	go func() { win.WriteString("a\na\na\nb\n"); win.Close() }()
	os.Stdin = rin
	os.Stdout = w
	rc := cmdLogs([]string{"--max", "10"})
	w.Close()
	os.Stdin, os.Stdout = oldIn, oldOut
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "a   (x3)") {
		t.Fatalf("dedupe missing: %q", out)
	}
	if !strings.Contains(out, "b") {
		t.Fatalf("expected b: %q", out)
	}
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
}

func TestTokensCmdOutput(t *testing.T) {
	oldIn, oldOut := os.Stdin, os.Stdout
	r, w, _ := os.Pipe()
	rin, win, _ := os.Pipe()
	go func() { win.WriteString("hello world"); win.Close() }()
	os.Stdin = rin
	os.Stdout = w
	rc := cmdTokens([]string{"-m", "gpt-4o-mini"})
	w.Close()
	os.Stdin, os.Stdout = oldIn, oldOut
	var buf bytes.Buffer
	buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "tokens:") {
		t.Fatalf("missing tokens: %q", out)
	}
	if rc != 0 {
		t.Fatalf("rc=%d", rc)
	}
}
