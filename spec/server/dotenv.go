// ABOUTME: Minimal .env file loader that sets environment variables from KEY=VALUE pairs.
// ABOUTME: Supports comments, blank lines, quoted values, and does not override existing env vars.
package server

import (
	"bufio"
	"os"
	"strings"
)

// LoadDotEnv reads a .env file and sets environment variables for any keys
// not already present in the environment. This preserves explicit env var
// overrides while providing defaults from the file.
//
// Lines starting with # are comments. Blank lines are ignored.
// Values may be optionally wrapped in single or double quotes.
// Returns nil if the file does not exist.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first '='
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		if key == "" {
			continue
		}
		value := strings.TrimSpace(line[idx+1:])

		// Strip matching quotes
		if len(value) >= 2 {
			first, last := value[0], value[len(value)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Only set if not already in environment
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}
