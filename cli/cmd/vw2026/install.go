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
	Plugin string `arg:"" placeholder:"NAME@TAG" help:"Plug-in to install and its tag, such as cli@latest."`
	Check  bool   `env:"VW2026_INSTALL_CHECK" negatable:"" help:"Only tell whether a new build is available; change nothing."`
	// sep:"none" で、URL のコンマで分けない（kong は既定で、引数と環境変数の値をコンマで分ける）。空白で
	// 分けるのは run の中（URL は空白を含まない）。
	Registry []string `env:"VW2026_REGISTRY" sep:"none" placeholder:"URL" help:"Registry (an https:// or a file:// URL) to look the name up in after the default one (repeatable; spaces separate several; an empty value is wrong usage)."`

	// 初回のスクリプトだけが渡す --plugins-dir（pluginsDirFlag）
	pluginsDirFlag `embed:""`
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
works while Vectorworks runs. When it cannot read that lock (other than because
it is not there), it cannot tell whether Vectorworks runs, so it changes
nothing and exits with 1.

It prints one line of JSON with one of these outcomes:

	{"outcome":"installed","name":"cli","requested":"latest","tag":"...",
	 "target":"...","registry":"default","plugins_dir":"...","hook":{...}}
	{"outcome":"up_to_date",...}
	{"outcome":"available",...}
	{"outcome":"vectorworks_running",...}
	{"outcome":"failed","name":"...","message":"..."}

With --check, "available" means a new build is available, none is installed, or
the installed one has not finished installing (its hook has not succeeded). In
the last case, run install with the installed tag first: while the previous
version waits in the work folder, install refuses another tag (the message says
how to give the pending version up by hand). "hook" is the output of the hook (a
JSON object), absent when the plug-in has none. On a failure (cannot download,
the sha256 does not match, the zip is wrong, the folder cannot be replaced, the
hook failed), install leaves <Plug-Ins> as it was, except for what the hook
changed outside the folder, writes the reason to the standard error too, and
exits with 1. The exception is a hook that fails while Vectorworks starts:
Vectorworks may use the new version, so install leaves it in <Plug-Ins>, says so
in the message, and exits with 1; quit Vectorworks, then run install with the
same tag again (it runs only the hook). Replacing the folder fails on Windows
while a program has a file in it open or has its current directory in it.

<Plug-Ins> is the folder of plug-ins that this vw2026 is installed in (it runs
as <Plug-Ins>/cli/bin/vw2026), so install and uninstall always work on the
folder that holds the running cli. A vw2026 outside such a folder (the
first-time script's, or one built by hand) uses the Plug-Ins folder of
Vectorworks 2026 for this user:
~/Library/Application Support/Vectorworks/2026/Plug-Ins on macOS and
%APPDATA%\Nemetschek\Vectorworks\2026\Plug-Ins on Windows. To use another
folder, give it to the first-time script (--plugins-dir <Plug-Ins>), which
passes it to its vw2026; an installed vw2026 refuses it. Install works in
vectorworks2026-cli-<key> in the temporary folder of the user (one for each
folder of plug-ins), which must be on the same volume as <Plug-Ins>.

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
		return deployFailed(e, c.Plugin, install.Usagef("%q has no tag: give NAME@TAG, such as cli@latest", c.Plugin))
	}
	// 書き方の誤りは、場所を求める前に使い方の誤りにする（場所が分からないときも終了コード 2）。
	if err := install.CheckRequest(name, tag); err != nil {
		return deployFailed(e, name, err)
	}
	// URL は空白を含まないので、値の中の空白で分ける（VW2026_REGISTRY に複数を書ける）。空の値（空白だけを
	// 含む）は使い方の誤りにする（--registry "$UNSET" のような取り違えを黙って通さない）。空の引数と空の
	// 環境変数は見分けられないので、どちらも断る（環境変数で消すときは unset する）。
	var registries []string
	for _, value := range c.Registry {
		urls := strings.Fields(value)
		if len(urls) == 0 {
			return deployFailed(e, name, install.Usagef("the registry is empty: give a URL to --registry, or unset VW2026_REGISTRY"))
		}
		registries = append(registries, urls...)
	}
	if err := install.CheckRegistries(registries); err != nil {
		return deployFailed(e, name, err)
	}
	options, err := deployOptions(e, c.PluginsDir)
	if err != nil {
		return deployFailed(e, name, err)
	}
	options.Registries = registries
	options.Target = install.Target()
	result, err := install.Install(options, name, tag, c.Check)
	if err != nil {
		if errors.Is(err, install.ErrRunning) {
			return deployRunning(e, err, result)
		}
		return deployFailed(e, name, err)
	}
	return emit(e, result)
}

// pluginsDirFlag は、初回のスクリプトだけが一時フォルダの CLI に渡す隠した指定（install と uninstall で同じ。help に
// 出さず、環境変数も持たない。入った CLI は自分の入った <Plug-Ins> を使い、これを断る）。与えていない（nil）と空の値
// （使い方の誤り）を分けるため、ポインタで受ける。
type pluginsDirFlag struct {
	PluginsDir *string `name:"plugins-dir" hidden:"" placeholder:"DIR" help:"Folder of plug-ins, for the first-time script only."`
}

// deployOptions は install / uninstall に渡す場所を env から求める。<Plug-Ins> は、入った CLI なら自分の入った
// フォルダ（install.PluginsDirOf）。フォルダの外の CLI（初回のスクリプトの一時フォルダのもの）なら、初回の
// スクリプトが渡した --plugins-dir、無ければ既定の場所。入った CLI に --plugins-dir を渡すのと、空の値は使い方の
// 誤り（入った CLI が別の <Plug-Ins> を扱うと、動いている cli と違う場所の Vectorworks のロックを見てしまう）。
func deployOptions(e *env, given *string) (install.Options, error) {
	// 走らせている CLI の場所が分からなければ、入った CLI かを決められないので、--plugins-dir が無ければ失敗に
	// する（既定の場所へ移らない）。--plugins-dir があれば、よけないだけにする（付け替えが失敗すれば何も変わらない）。
	executable, exeErr := e.executable()
	own, installed := install.PluginsDirOf(executable)
	var pluginsDir string
	var err error
	switch {
	case given != nil && installed:
		return install.Options{}, install.Usagef("--plugins-dir is only for the first-time script; this vw2026 is "+
			"installed in %s and works on that folder", own)
	case given != nil && *given == "":
		return install.Options{}, install.Usagef("the folder of plug-ins is empty: give an absolute path to --plugins-dir")
	case given != nil:
		pluginsDir = *given
	case installed:
		pluginsDir = own
	case exeErr != nil:
		return install.Options{}, fmt.Errorf("cannot locate this vw2026, so the folder of plug-ins it is installed in "+
			"is unknown: %w", exeErr)
	default:
		if pluginsDir, err = e.pluginsDir(); err != nil {
			return install.Options{}, fmt.Errorf("cannot locate the folder of plug-ins: %w", err)
		}
	}
	work, err := e.workDir()
	if err != nil {
		return install.Options{}, fmt.Errorf("cannot locate the work folder: %w", err)
	}
	return install.Options{
		PluginsDir: pluginsDir,
		// <Work> は <Plug-Ins> ごとに分ける（install.WorkDirFor）。
		WorkDir:    install.WorkDirFor(work, pluginsDir),
		Executable: executable,
		Stderr:     e.stderr,
		Terminal:   isTerminal(e.stderr),
	}, nil
}

// isTerminal は、w が端末か。取得の進み具合を同じ行に書き直すかを決める（ログ・パイプ・/dev/null や NUL の
// ような端末でない文字型のデバイスには 1 行ずつ足す）。端末かは OS に尋ねる（terminal_*.go）。
func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && terminal(file.Fd())
}

// deployRunning は、Vectorworks が動いていて何もしなかったこと（ErrRunning）を標準エラーと標準出力の JSON（result）に
// 書き、終了コード 6 を返す。
func deployRunning(e *env, err error, result any) int {
	fmt.Fprintf(e.stderr, "vw2026: %v\n", err)
	emit(e, result)
	return exitRunning
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
