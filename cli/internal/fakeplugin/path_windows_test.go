//go:build windows

package fakeplugin

import "testing"

// extendedPath は、プラグインの extended（src/core/FilesWindows.cpp）と同じく \\?\ を付ける。
func TestExtendedPath(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`C:\spool`, `\\?\C:\spool`},
		{`C:/spool/a`, `\\?\C:\spool\a`},
		{`\\server\share\spool`, `\\?\UNC\server\share\spool`},
		{`\\?\C:\spool`, `\\?\C:\spool`},
	} {
		if got := extendedPath(c.in); got != c.want {
			t.Errorf("extendedPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
