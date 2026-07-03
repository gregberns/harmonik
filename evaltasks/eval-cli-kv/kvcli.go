// Package kvcli implements a stateful key-value CLI over a JSON file.
// Subcommands: set <key> <value>, get <key>, del <key>, list.
// Usage flag: --store <path> (default "kv.json" relative to CWD).
// Exit codes: 0=ok, 1=key-not-found or store error, 2=usage error.
package kvcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

// Run is the in-process entry point. Returns an exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	storePath := "kv.json"
	rest := args
	for i := 0; i+1 < len(rest); i++ {
		if rest[i] == "--store" {
			storePath = rest[i+1]
			rest = append(append([]string{}, rest[:i]...), rest[i+2:]...)
			break
		}
	}

	if len(rest) == 0 {
		fmt.Fprintln(stderr, "usage: kvcli [--store <path>] <set|get|del|list> [args...]")
		return 2
	}

	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "set":
		if len(cmdArgs) != 2 {
			fmt.Fprintln(stderr, "usage: kvcli set <key> <value>")
			return 2
		}
		return cmdSet(storePath, cmdArgs[0], cmdArgs[1], stdout, stderr)
	case "get":
		if len(cmdArgs) != 1 {
			fmt.Fprintln(stderr, "usage: kvcli get <key>")
			return 2
		}
		return cmdGet(storePath, cmdArgs[0], stdout, stderr)
	case "del":
		if len(cmdArgs) != 1 {
			fmt.Fprintln(stderr, "usage: kvcli del <key>")
			return 2
		}
		return cmdDel(storePath, cmdArgs[0], stdout, stderr)
	case "list":
		return cmdList(storePath, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", cmd)
		return 2
	}
}

func loadStore(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read store: %w", err)
	}
	if len(data) == 0 {
		return map[string]string{}, nil
	}
	var store map[string]string
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("malformed store: %w", err)
	}
	return store, nil
}

func saveStore(path string, store map[string]string) error {
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func cmdSet(path, key, value string, stdout, stderr io.Writer) int {
	store, err := loadStore(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store[key] = value
	if err := saveStore(path, store); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "set %s\n", key)
	return 0
}

func cmdGet(path, key string, stdout, stderr io.Writer) int {
	store, err := loadStore(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	val, ok := store[key]
	if !ok {
		fmt.Fprintf(stderr, "key not found: %s\n", key)
		return 1
	}
	fmt.Fprintln(stdout, val)
	return 0
}

func cmdDel(path, key string, stdout, stderr io.Writer) int {
	store, err := loadStore(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	delete(store, key)
	if err := saveStore(path, store); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "del %s\n", key)
	return 0
}

func cmdList(path string, stdout, stderr io.Writer) int {
	store, err := loadStore(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	keys := make([]string, 0, len(store))
	for k := range store {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(stdout, "%s\t%s\n", k, store[k])
	}
	return 0
}
