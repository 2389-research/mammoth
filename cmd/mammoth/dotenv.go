// ABOUTME: Loads environment variables from a .env file at startup.
// ABOUTME: Sets variables only when not already present in the environment (no clobber).
package main

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv reads a .env file and sets any variables not already in the environment.
// Missing files are silently ignored. Lines starting with # are comments.
// Supports KEY=VALUE, KEY="VALUE", KEY='VALUE', and export KEY=VALUE.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		// Split on first '=' only — values can contain '='.
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		// Strip matching quotes from value.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		// Only set if not already in the environment.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}
