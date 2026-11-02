package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
)

type uninstallCmd struct {
	Name string `arg:"" help:"Plug-in to uninstall, such as cli."`

	// 初回のスクリプトだけが渡す --plugins-dir（pluginsDirFlag）
	pluginsDirFlag `embed:""`
}

func (uninstallCmd) Help() string {
	return `
Uninstall removes a plug-in that vw2026 installed
(docs/plugin/install-and-update.md). It reads only the record in
<Plug-Ins>/<NAME>/, not the registries, so it works offline and for a plug-in
that left the registry. It removes a folder only when the record names the
folder and the module of the record is there; otherwise it removes nothing.
Like install, it works on the folder of plug-ins that this vw2026 is installed
in (see "vw2026 help install").

	vw2026 uninstall my-plugin

Uninstall does not remove the folder that this vw2026 runs from, such as cli
for the vw2026 on the PATH (<Plug-Ins>/cli/bin/vw2026): once cli is
uninstalled, vw2026 is not used any more. It changes nothing, exits with 2,
and shows the first-time script of the installed tag to run instead, which
uninstalls cli with a vw2026 of that tag outside the folder (and leaves nothing
behind; the script of a release always uses the vw2026 of that release). On
macOS and on Windows:

	curl -fsSL ` + releases + `download/<TAG>/get-vw2026.sh | sh -s -- uninstall
	& ([scriptblock]::Create((irm ` + releases + `download/<TAG>/get-vw2026.ps1))) uninstall

A prerelease is deleted when its pull request closes; then use the script of
latest, which uninstalls with the vw2026 of latest:

	curl -fsSL ` + releases + `latest/download/get-vw2026.sh | sh -s -- uninstall

Uninstall first moves the folder out of <Plug-Ins>, then runs the uninstall
hook of the installed version (for cli, it takes vw2026 off the PATH), then
deletes the folder. If the hook fails, the folder goes back. If uninstall
stops after moving the folder (Ctrl-C, power loss), run it again: it goes on
from the hook, and if the hook fails then, the folder stays moved out (it may
be partly deleted), and the message says where it is. If Vectorworks starts
while the hook runs, the folder stays moved out and uninstall fails (exit
status 1); quit Vectorworks, then run uninstall again: it goes on from the
hook.

Like install, uninstall changes nothing while Vectorworks is running with the
bridge (exit status 6), and a plug-in other than cli is uninstalled only from a
folder of plug-ins that has cli. Like install, it checks the cli first: when
that folder has no cli that vw2026 installed, it fails with 1 even while
Vectorworks is running (quitting Vectorworks would not help). Uninstalling cli
does not refuse while other plug-ins remain, but without vw2026 they can no
longer be uninstalled; see "remaining".

It prints one line of JSON with one of these outcomes:

	{"outcome":"uninstalled","name":"my-plugin","hook":{...},
	 "remaining":[...],"left":[...]}
	{"outcome":"not_installed","name":"...","remaining":[...],"left":[]}
	{"outcome":"vectorworks_running",...}
	{"outcome":"failed","name":"...","message":"...","command":"..."}

"remaining" lists the plug-ins that vw2026 installed and that remain in
<Plug-Ins>. "left" lists the moved-away copies of vw2026 (left by an update on
Windows) that could not be deleted. They are in the temporary folder of the
user; the next install or uninstall deletes them, and so does the cleanup of
temporary files of Windows if it is turned on (Storage Sense, Disk Cleanup).
"command" is the first-time script (of the installed tag) to run when this
vw2026 runs from the folder.

Exit status: 0 success (including not_installed), 1 failure, 2 wrong usage
(including a vw2026 that runs from the folder), 6 Vectorworks is running.
`
}

func (c *uninstallCmd) run(e *env) int {
	// 書き方の誤りは、場所を求める前に使い方の誤りにする（場所が分からないときも終了コード 2）。
	if err := install.CheckName(c.Name); err != nil {
		return deployFailed(e, c.Name, err)
	}
	options, err := deployOptions(e, c.PluginsDir)
	if err != nil {
		return deployFailed(e, c.Name, err)
	}
	result, err := install.Uninstall(options, c.Name)
	var inside *install.RunsInsideError
	switch {
	case errors.Is(err, install.ErrRunning):
		return deployRunning(e, c.Name, err, result)
	case errors.As(err, &inside):
		// 入れた cli の vw2026 では外さず、入っているタグの初回のスクリプトと CLI で外すよう案内する。
		// PR のプレリリースは PR を閉じると消えるので、latest で外すコマンドも示す。
		// 外す CLI は入った cli なので、<Plug-Ins> は自分の入ったフォルダ。それが既定の場所でなければ、初回の
		// スクリプトに渡すよう --plugins-dir をコマンドに載せる（既定の場所なら書かない）。
		given := ""
		if !isDefaultPlugins(e, options.PluginsDir) {
			given = options.PluginsDir
		}
		command := uninstallScript(runtime.GOOS, inside.Tag, given)
		fmt.Fprintf(e.stderr, "vw2026: %v. vw2026 is not used once %s is uninstalled; run the first-time script "+
			"of the installed tag instead:\n\t%s\nIf that release no longer exists (a prerelease is deleted when "+
			"its pull request closes), use latest:\n\t%s\n",
			err, c.Name, command, uninstallScript(runtime.GOOS, "latest", given))
		emit(e, map[string]any{"outcome": "failed", "name": c.Name, "message": err.Error(), "command": command})
		return exitUsage
	case err != nil:
		return deployFailed(e, c.Name, err)
	}
	return emit(e, result)
}

// releases は、初回のスクリプトを置くリリースの場所。
const releases = "https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/"

// powershellQuote は、PowerShell の単一引用符の文字列の中身にする。PowerShell は ' のほかに活字の引用符
// （U+2018・U+2019・U+201A・U+201B）も単一引用符の区切りとして扱うので、どれも同じ文字を 2 つ重ねて逃がす。
var powershellQuote = strings.NewReplacer("'", "''", "\u2018", "\u2018\u2018", "\u2019", "\u2019\u2019",
	"\u201a", "\u201a\u201a", "\u201b", "\u201b\u201b")

// uninstallScript は、goos で tag のリリースの初回のスクリプトに uninstall を渡すコマンド（どのリリースの
// スクリプトも、そのリリースの CLI で cli を外す）。tag が latest なら latest のリリースのもの。
// --plugins-dir を渡されていれば、そのまま渡す（シェルの引用符で囲む）。
func uninstallScript(goos, tag, pluginsDir string) string {
	base := releases + "download/" + tag + "/"
	if tag == "latest" {
		base = releases + "latest/download/"
	}
	if goos == "windows" {
		command := "& ([scriptblock]::Create((irm " + base + "get-vw2026.ps1))) uninstall"
		if pluginsDir != "" {
			command += " --plugins-dir '" + powershellQuote.Replace(pluginsDir) + "'"
		}
		return command
	}
	command := "curl -fsSL " + base + "get-vw2026.sh | sh -s -- uninstall"
	if pluginsDir != "" {
		command += " --plugins-dir '" + strings.ReplaceAll(pluginsDir, "'", `'\''`) + "'"
	}
	return command
}

// isDefaultPlugins は、dir が既定の <Plug-Ins> と同じフォルダか（書き方ではなくフォルダの同一性で決める。分からなければ偽）。
func isDefaultPlugins(e *env, dir string) bool {
	def, err := e.pluginsDir()
	if err != nil {
		return false
	}
	return install.SameDirectory(dir, def)
}
