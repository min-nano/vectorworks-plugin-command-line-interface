package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/install"
)

type uninstallCmd struct {
	Name string `arg:"" help:"Plug-in to uninstall, such as cli."`

	// 初回スクリプトのみが渡す --plugins-dir（pluginsDirFlag）
	pluginsDirFlag `embed:""`
}

func (uninstallCmd) Help() string {
	return edition.Expand(`
Uninstall removes a plug-in that ${cmd} installed
(docs/plugin/install-and-update.md). It reads only the record in
<Plug-Ins>/<NAME>/, not the registries, so it works offline and for a plug-in
that left the registry. It removes a folder only when the record names the
folder and the module of the record is there; otherwise it removes nothing.
Like install, it works on the folder of plug-ins that this ${cmd} is installed
in (see "${cmd} help install").

	${cmd} uninstall my-plugin

Uninstall does not remove the folder that this ${cmd} runs from, such as cli
for the ${cmd} on the PATH (<Plug-Ins>/cli/bin/${cmd}): once cli is
uninstalled, ${cmd} is not used any more. It changes nothing, exits with 2,
and shows the first-time script of the installed tag to run instead, which
uninstalls cli with a ${cmd} of that tag outside the folder (and leaves nothing
behind; the script of a release always uses the ${cmd} of that release). On
macOS and on Windows:

	curl -fsSL ` + releases + `download/<TAG>/${bootstrap}.sh | sh -s -- uninstall
	& ([scriptblock]::Create((irm ` + releases + `download/<TAG>/${bootstrap}.ps1))) uninstall

A prerelease is deleted when its pull request closes; then use the script of
latest, which uninstalls with the ${cmd} of latest:

	curl -fsSL ` + releases + `latest/download/${bootstrap}.sh | sh -s -- uninstall

Uninstall first moves the folder out of <Plug-Ins>, then runs the uninstall
hook of the installed version (for cli, it takes ${cmd} off the PATH), then
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
that folder has no cli that ${cmd} installed, it fails with 1 even while
Vectorworks is running. If cli is missing because an install or uninstall of
cli stopped halfway, quit Vectorworks and finish that first with the
first-time script (with --plugins-dir for a folder other than the default; see
${cmd} help install). Uninstalling cli
does not refuse while other plug-ins remain, but without ${cmd} they can no
longer be uninstalled; see "remaining".

It prints one line of JSON with one of these outcomes:

	{"outcome":"uninstalled","name":"my-plugin","hook":{...},
	 "remaining":[...],"left":[...]}
	{"outcome":"not_installed","name":"...","remaining":[...],"left":[]}
	{"outcome":"vectorworks_running",...}
	{"outcome":"failed","name":"...","message":"...","command":"..."}

"remaining" lists the plug-ins that ${cmd} installed and that remain in
<Plug-Ins>. "left" lists the moved-away copies of ${cmd} (left by an update on
Windows) that could not be deleted. They are in the temporary folder of the
user; the next install or uninstall deletes them, and so does the cleanup of
temporary files of Windows if it is turned on (Storage Sense, Disk Cleanup).
"command" is the first-time script (of the installed tag) to run when this
${cmd} runs from the folder.

Exit status: 0 success (including not_installed), 1 failure, 2 wrong usage
(including a ${cmd} that runs from the folder), 6 Vectorworks is running.
`)
}

func (c *uninstallCmd) run(e *env) int {
	// 記法の誤りは、場所を導出する前に使用方法の誤りとする（場所が不明な場合も終了コード 2）。
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
		return deployRunning(e, err, result)
	case errors.As(err, &inside):
		// インストールした cli の vw2026 ではアンインストールせず、インストールされているタグの
		// 初回スクリプトと CLI でアンインストールするよう案内する。
		// PR のプレリリースは PR を閉じると削除されるため、latest でアンインストールするコマンドも
		// 示す。
		// アンインストールする CLI はインストールされた cli であるため、<Plug-Ins> は自身の
		// インストール先のフォルダ。それが既定の場所でなければ、初回スクリプトに渡すよう
		// --plugins-dir をコマンドに含める（既定の場所ならば記述しない）。
		given := ""
		if !isDefaultPlugins(e, options.PluginsDir) {
			given = options.PluginsDir
		}
		command := uninstallScript(runtime.GOOS, inside.Tag, given)
		fmt.Fprintf(e.stderr, edition.Command+": %v. "+edition.Command+" is not used once %s is uninstalled; run the first-time script "+
			"of the installed tag instead:\n\t%s\nIf that release no longer exists (a prerelease is deleted when "+
			"its pull request closes), use latest:\n\t%s\n",
			err, c.Name, command, uninstallScript(runtime.GOOS, "latest", given))
		out := failedJSON(c.Name, err)
		out["command"] = command
		emit(e, out)
		return exitUsage
	case err != nil:
		return deployFailed(e, c.Name, err)
	}
	return emit(e, result)
}

// releases は、初回スクリプトを配置するリリースの場所。
const releases = "https://github.com/min-nano/vectorworks-plugin-command-line-interface/releases/"

// powershellQuote は、PowerShell の単一引用符の文字列の内容に変換する。PowerShell は ' の他に活字の
// 引用符（U+2018・U+2019・U+201A・U+201B）も単一引用符の区切りとして扱うため、いずれも同じ文字を
// 2 つ重ねてエスケープする。
var powershellQuote = strings.NewReplacer("'", "''", "\u2018", "\u2018\u2018", "\u2019", "\u2019\u2019",
	"\u201a", "\u201a\u201a", "\u201b", "\u201b\u201b")

// uninstallScript は、goos で tag のリリースの初回スクリプトに uninstall を渡すコマンド（いずれの
// リリースのスクリプトも、そのリリースの CLI で cli をアンインストールする）。tag が latest ならば
// latest のリリースのもの。--plugins-dir を渡されていれば、そのまま渡す（シェルの引用符で囲む）。
func uninstallScript(goos, tag, pluginsDir string) string {
	base := releases + "download/" + tag + "/"
	if tag == "latest" {
		base = releases + "latest/download/"
	}
	if goos == "windows" {
		command := "& ([scriptblock]::Create((irm " + base + edition.Bootstrap + ".ps1))) uninstall"
		if pluginsDir != "" {
			command += " --plugins-dir '" + powershellQuote.Replace(pluginsDir) + "'"
		}
		return command
	}
	command := "curl -fsSL " + base + edition.Bootstrap + ".sh | sh -s -- uninstall"
	if pluginsDir != "" {
		command += " --plugins-dir '" + strings.ReplaceAll(pluginsDir, "'", `'\''`) + "'"
	}
	return command
}

// isDefaultPlugins は、dir が既定の <Plug-Ins> と同一のフォルダか否か（表記ではなくフォルダの
// 同一性で判定する。不明ならば偽）。
func isDefaultPlugins(e *env, dir string) bool {
	def, err := e.pluginsDir()
	if err != nil {
		return false
	}
	return install.SameDirectory(dir, def)
}
