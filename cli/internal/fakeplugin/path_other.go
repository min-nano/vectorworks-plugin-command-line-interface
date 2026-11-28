//go:build !windows

package fakeplugin

// extendedPath は Windows のみのもの（path_windows.go）。他ではそのまま返す。
func extendedPath(dir string) string { return dir }
