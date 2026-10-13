package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// LoadDotEnv copies KEY=VALUE pairs from a file into the process environment.
// Variables that are already set win, so real environment configuration in
// production is never overridden by a stray file. A missing file is not an error.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open env file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		if err := setEnvLine(scanner.Text()); err != nil {
			return fmt.Errorf("env file line %d: %w", line, err)
		}
	}
	return scanner.Err()
}

func setEnvLine(line string) error {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil
	}
	key, value, found := strings.Cut(strings.TrimPrefix(line, "export "), "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return errors.New("expected KEY=VALUE")
	}
	if _, exists := os.LookupEnv(key); exists {
		return nil
	}
	return os.Setenv(key, unquote(strings.TrimSpace(value)))
}

func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if first == last && (first == '"' || first == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}
