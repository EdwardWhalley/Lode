package sqlite_test

import "os"

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
