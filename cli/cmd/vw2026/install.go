package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
)

type installCmd struct {
	Plugin     string `arg:"" placeholder:"NAME@TAG" help:"Plug-in to install and its tag, such as cli@latest."`
	Check      bool   `env:"VW2026_INSTALL_CHECK" negatable:"" help:"Only tell whether a new build is available; change nothing."`
	PluginsDir string `env:"VW2026_PLUGINS_DIR" placeholder:"DIR" help:"Folder of plug-ins, as an absolute path (default: the Plug-Ins folder of Vectorworks 2026 for this user)."`
	// sep:"none" で、URL のコンマで分けない（kong は既定で、引数と環境変数の値をコンマで分ける）。空白で
	// 分けるのは run の中（URL は空白を含まない）。
	Registry []string `env:"VW2026_REGISTRY" sep:"none" placeholder:"URL" help:"Registry (an https:// or a file:// URL) to look the name up in after the default one (repeatable; spaces separate several)."`
}

func (installCmd) Help() string {
	return `
Install installs one plug-in, or updates it, from its release
(docs/plugin/install-and-update.md). The same command installs cli itself
and any other plug-in in a registry (docs/registry.md). Both the name and the
tag are required; there is no default tag.

	vw2026 install cli@latest
	vw2026 install cli@dev-feature-x-61-a1b2c3d
	vw2026 install cli@latest --check
	vw2026 install my-plugin@2026.3.1 --registry https://example.com/plugins.json
	vw2026 install my-plugin@2026.3.1 --registry file:///Users/me/plugins.json

Install looks the name up in the registry built into vw2026, then in each
--registry in order, and uses the first one that has it. A registry is an
https:// or a file:// URL; write a path as file:///... (on Windows
file:///C:/...), with %20 for a space. --registry can be repeated, and spaces
separate registries in a value (a URL has no space), so VW2026_REGISTRY can
hold several. The first rule of that entry
that matches the tag gives the manifest; the manifest gives the zip and its
sha256. The plug-in goes into <Plug-Ins>/<NAME>/ with a record of the install
(vw2026-install.json), replacing the folder as a whole, and then its install
hook runs: for cli, the hook puts vw2026 on the PATH. Install does not touch a
folder of that name that vw2026 did not install, or that came from another
registry. A plug-in other than cli goes only into a folder of plug-ins that
has cli.

When the installed plug-in already has the tag (and the target), install
neither downloads the zip nor replaces the folder: it runs the install hook of
the installed version again and prints up_to_date (this also finishes an
install whose hook failed while Vectorworks started: once the hook succeeds, it
cleans up the previous version left in the work folder). To reinstall the same
tag, uninstall first. The tag latest names whatever the release calls latest, so
install fetches the manifest to learn the tag.

Install changes nothing while Vectorworks is running with the bridge (it holds
the lock in <Plug-Ins>/cli/spool/): it prints {"outcome":"vectorworks_running"}
and exits with 6, even for the same tag. It does not quit Vectorworks and does
not wait; quit Vectorworks first (see "vw2026 --help" for the sequence). --check
works while Vectorworks runs.

It prints one line of JSON with one of these outcomes:

	{"outcome":"installed","name":"cli","requested":"latest","tag":"...",
	 "target":"...","registry":"default","plugins_dir":"...","hook":{...}}
	{"outcome":"up_to_date",...}
	{"outcome":"available",...}
	{"outcome":"vectorworks_running",...}
	{"outcome":"failed","message":"..."}

With --check, "available" means a new build is available, or none is
installed. "hook" is the output of the hook (a JSON object), absent when the plug-in has
none. On a failure (cannot download, the sha256 does not match, the zip is
wrong, the folder cannot be replaced, the hook failed), install leaves
<Plug-Ins> as it was, except for what the hook changed outside the folder,
writes the reason to the standard error too, and exits with 1. Replacing the
folder fails on Windows while a program has a file in it open or has its
current directory in it.

The default <Plug-Ins> is
~/Library/Application Support/Vectorworks/2026/Plug-Ins on macOS and
%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins on Windows. vw2026
does not remember another one: give --plugins-dir to every install and
uninstall. Install works in vectorworks2026-cli in the temporary folder of the
user, which must be on the same volume as <Plug-Ins>.

Install has no time limit on the network or the hook: stop it (Ctrl-C) or limit
it from the caller. Stopped while it downloads, it has changed only the work
folder. While it downloads the zip, it writes how much has arrived to the
standard error: one line rewritten in place on a terminal, otherwise a line
every 10 seconds, then "downloaded <size>".

Do not run install or uninstall at the same time as another install or
uninstall. Exit status: 0 success (including up_to_date and available), 1
failure, 2 wrong usage, 6 Vectorworks is running.
`
}

func (c *installCmd) run(e *env) int {
	name, tag, ok := strings.Cut(c.Plugin, "@")
	if !ok {
		fmt.Fprintf(e.stderr, "vw2026: %q has no tag: give NAME@TAG, such as cli@latest\n", c.Plugin)
		return exitUsage
	}
	options, code := deployOptions(e, c.PluginsDir)
	if code != exitOK {
		return code
	}
	// URL は空白を含まないので、値の中の空白で分ける（VW2026_REGISTRY に複数を書ける）。
	for _, value := range c.Registry {
		options.Registries = append(options.Registries, strings.Fields(value)...)
	}
	options.Target = install.Target()
	result, err := install.Install(options, name, tag, c.Check)
	if err != nil {
		if errors.Is(err, install.ErrRunning) {
			fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
			emit(e, result)
			return exitRunning
		}
		return deployFailed(e, name, err)
	}
	return emit(e, result)
}

// deployOptions は install / uninstall に渡す場所を env から求める。
func deployOptions(e *env, pluginsDir string) (install.Options, int) {
	var err error
	if pluginsDir == "" {
		if pluginsDir, err = e.pluginsDir(); err != nil {
			fmt.Fprintf(e.stderr, "vw2026: cannot locate the folder of plug-ins (give --plugins-dir): %v\n", err)
			return install.Options{}, exitFailure
		}
	}
	work, err := e.workDir()
	if err != nil {
		fmt.Fprintf(e.stderr, "vw2026: cannot locate the work folder: %v\n", err)
		return install.Options{}, exitFailure
	}
	// 走らせている CLI の場所が分からなければ、よけない（付け替えが失敗すれば何も変わらない）。
	executable, _ := e.executable()
	return install.Options{
		PluginsDir: pluginsDir,
		WorkDir:    work,
		Executable: executable,
		Stderr:     e.stderr,
		Terminal:   isTerminal(e.stderr),
	}, exitOK
}

// isTerminal は、w が端末か。取得の進み具合を同じ行に書き直すかを決める（ログ・パイプ・/dev/null や NUL の
// ような端末でない文字型のデバイスには 1 行ずつ足す）。端末かは OS に尋ねる（terminal_*.go）。
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && terminal(file.Fd())
}

// deployFailed は失敗を標準出力の JSON（message）と標準エラーに書き、終了コードを返す。
func deployFailed(e *env, name string, err error) int {
	fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
	emit(e, map[string]any{"outcome": "failed", "name": name, "message": err.Error()})
	if install.IsUsage(err) {
		return exitUsage
	}
	return exitFailure
}
