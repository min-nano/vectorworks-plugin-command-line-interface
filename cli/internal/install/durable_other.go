//go:build !darwin

package install

import "os"

// fsyncEntry は、展開したファイルを Sync する（mac の他では File.Sync と同じ。durable.go）。
func fsyncEntry(file *os.File) error {
	return file.Sync()
}
