package shave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cacheRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".shave", "cache")
}
func kvDir() string  { return filepath.Join(cacheRoot(), "kv") }
func runDir() string { return filepath.Join(cacheRoot(), "run") }

func cmdCache(args []string) int {
	if len(args) == 0 {
		fmt.Println("usage: shave cache [get|set|stats|clear] ...")
		return 2
	}
	switch args[0] {
	case "get":
		if len(args) < 2 {
			fmt.Println("usage: shave cache get <key>")
			return 2
		}
		key := sanitizeKey(args[1])
		p := filepath.Join(kvDir(), key+".txt")
		b, err := os.ReadFile(p)
		if err != nil {
			return 1
		}
		os.Stdout.Write(b)
		return 0
	case "set":
		if len(args) < 2 {
			fmt.Println("usage: shave cache set <key>")
			return 2
		}
		key := sanitizeKey(args[1])
		if err := os.MkdirAll(kvDir(), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		p := filepath.Join(kvDir(), key+".txt")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("cached %d bytes under %s\n", len(b), key)
		return 0
	case "stats":
		total := 0
		count := 0
		_ = filepath.Walk(cacheRoot(), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				count++
				total += int(info.Size())
			}
			return nil
		})
		fmt.Printf("entries: %d\nbytes:   %d\nroot:    %s\n", count, total, cacheRoot())
		return 0
	case "clear":
		os.RemoveAll(cacheRoot())
		fmt.Println("cache cleared")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "shave: unknown cache subcommand %s\n", args[0])
		return 2
	}
}

func sanitizeKey(k string) string {
	k = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '_'
	}, k)
	if k == "" {
		k = "default"
	}
	return k
}

func cmdRun(args []string) int {
	i := 0
	for i < len(args) && args[i] != "--" {
		i++
	}
	if i >= len(args) || i+1 >= len(args) {
		fmt.Println("usage: shave run -- <cmd> [args...]")
		return 2
	}
	cmdArgs := args[i+1:]
	if len(cmdArgs) == 0 {
		fmt.Println("usage: shave run -- <cmd> [args...]")
		return 2
	}
	keySeed := strings.Join(cmdArgs, " ")
	h := sha256.Sum256([]byte(keySeed))
	key := hex.EncodeToString(h[:])[:16]

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	out, err := cmd.CombinedOutput()
	oh := sha256.Sum256(out)
	outHash := hex.EncodeToString(oh[:])

	_ = os.MkdirAll(runDir(), 0o755)
	hashPath := filepath.Join(runDir(), key+".hash")

	prevHash, _ := os.ReadFile(hashPath)
	if string(prevHash) == outHash && len(prevHash) > 0 {
		fmt.Printf("[unchanged: %d bytes cached for: %s]\n", len(out), keySeed)
		if err != nil {
			return 1
		}
		return 0
	}
	outPath := filepath.Join(runDir(), key+".out")
	_ = os.WriteFile(outPath, out, 0o644)
	_ = os.WriteFile(hashPath, []byte(outHash), 0o644)
	os.Stdout.Write(out)
	if err != nil {
		return 1
	}
	return 0
}
