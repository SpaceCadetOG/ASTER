package main

// A tiny stdlib-only file watcher that restarts a child command when Go files change.
// Usage:
//   go run ./internal/dev/watch.go -- go run ./cmd/bot
// Flags:
//   -q   : quiet child output (only restart notices)

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func snapshotHash(roots []string) ([]byte, error) {
	h := sha256.New()
	for _, root := range roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			} // ignore errors
			if d.IsDir() {
				base := filepath.Base(path)
				if strings.HasPrefix(base, ".git") || base == "node_modules" || base == "bin" {
					return filepath.SkipDir
				}
				return nil
			}
			if !(strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".mod") || strings.HasSuffix(path, ".sum")) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			// mix path + size + mtime into hash (fast & good enough)
			fmt.Fprintln(h, path, info.Size(), info.ModTime().UnixNano())
			return nil
		})
	}
	return h.Sum(nil), nil
}

func runChild(cmdArgs []string, quiet bool) (*exec.Cmd, *bytes.Buffer) {
	var buf *bytes.Buffer
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if quiet {
		buf = &bytes.Buffer{}
		cmd.Stdout = buf
		cmd.Stderr = buf
	}
	_ = cmd.Start()
	return cmd, buf
}

func main() {
	quiet := flag.Bool("q", false, "quiet child output")
	flag.Parse()
	sep := "--"

	// Find separator: everything after "--" is the child command
	args := os.Args[1:]
	after := []string{}
	if i := indexOf(args, sep); i >= 0 {
		after = args[i+1:]
	} else {
		after = args
	}
	if len(after) == 0 {
		fmt.Println("usage: go run internal/dev/watch.go -- <command> [args...]")
		os.Exit(2)
	}

	// Polling watcher loop
	roots := []string{"."}
	var prev []byte
	var err error
	prev, err = snapshotHash(roots)
	if err != nil {
		fmt.Println("initial snapshot error:", err)
		os.Exit(1)
	}

	fmt.Println("devwatch: starting:", strings.Join(after, " "))
	child, _ := runChild(after, *quiet)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		cur, err := snapshotHash(roots)
		if err != nil {
			continue
		}
		if !bytes.Equal(cur, prev) {
			// change detected → restart child
			prev = cur
			fmt.Println("\n—— devwatch: change detected → restarting ——")
			if child != nil && child.Process != nil {
				_ = child.Process.Kill()
				_, _ = child.Process.Wait()
			}
			child, _ = runChild(after, *quiet)
		}
	}
}

func indexOf(a []string, s string) int {
	for i, v := range a {
		if v == s {
			return i
		}
	}
	return -1
}
