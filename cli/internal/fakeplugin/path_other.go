//go:build !windows

package fakeplugin

// extendedPath は Windows だけのもの（path_windows.go）。ほかではそのまま返す。
func extendedPath(dir string) string { return dir }
