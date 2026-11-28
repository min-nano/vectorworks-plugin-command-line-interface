package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
)

// UninstallResult は uninstall の出力。
type UninstallResult struct {
	Outcome string          `json:"outcome"`
	Name    string          `json:"name"`
	Hook    json.RawMessage `json:"hook,omitempty"`
	// 残存した記録のある <Plug-Ins> の直下のフォルダの名前（全ての名前を出力する）
	Remaining []string `json:"remaining"`
	// 削除できなかった退避した CLI
	Left []string `json:"left"`
}

// Uninstall は <name> をアンインストールする
// （docs/plugin/install-and-update.md「アンインストール」）。レジストリは読み取らず、
// 記録のみで決定する。フックより先にフォルダを <Work>/old/<name>/ へ移動し、フックが失敗したら
// 戻す。Vectorworks が動作していれば何もせずに ErrRunning を返す。
func Uninstall(o Options, name string) (*UninstallResult, error) {
	// cmd も同じ CheckName で先に検証するが、名前から削除するパスを構築するため、
	// ここでも検証する（install と同様）。
	if err := CheckName(name); err != nil {
		return nil, err
	}
	p, err := o.places(name)
	if err != nil {
		return nil, err
	}
	// install と同じ precheck で、cli の記録を Vectorworks が動作しているかより先に検証する。
	// ErrRunning（何もしていない）を返すのはここのみであり、後で判明した場合は何かを変更した
	// 後であるため失敗とする。
	if err := precheck(p, name, true); errors.Is(err, ErrRunning) {
		return &UninstallResult{Outcome: outcomeRunning, Name: name, Remaining: []string{}, Left: []string{}}, err
	} else if err != nil {
		return nil, err
	}

	record, resumed, err := o.takeOut(p, name)
	if err != nil {
		return nil, err
	}
	if record == nil {
		// 前の回が削除しきれなかった構築の場所（discard の残存物）・継続先の無い標識・退避した
		// CLI・空の <Work> も、アンインストールした場合と同様に整理する（初回スクリプトの
		// uninstall が、インストールされていなくても何も残置しないように）。
		_ = removeStaging(p)
		return finishUninstall(p, name, outcomeNotInstalled, nil), nil
	}

	hook, err := o.runHook("uninstall", record.Hooks.Uninstall, hookEnv{
		name: name, tag: record.Tag, target: record.Target, pluginDir: p.oldDir, installDir: p.dest,
	})
	if err != nil {
		// 続きから実行された回は戻さない（OS の整理で一部のみ残存したフォルダかもしれない）。
		if resumed {
			return nil, fmt.Errorf("%w; %s is left as it is (run uninstall again, or delete it and %s)", err, p.oldDir, p.marker)
		}
		// 戻す前にロックを確認する（install の undoHook と同様）。フックの間に起動した
		// Vectorworks の下へフォルダを戻さず、標識を残置して、終了してからの uninstall が続きから
		// 実行できるようにする。
		if running(p) {
			return nil, fmt.Errorf("%w; %s, so %s stays in %s: quit Vectorworks, then run uninstall again "+
				"(it goes on from the hook)", err, startedMeanwhile, name, p.oldDir)
		}
		if renameErr := rename(p.oldDir, p.dest); renameErr != nil {
			return nil, fmt.Errorf("%w; %s cannot be put back (%v): move it back to %s by hand", err, p.oldDir, renameErr, p.dest)
		}
		_ = removeFile(p.marker)
		return nil, fmt.Errorf("%w; %s is put back", err, p.dest)
	}
	// フックの間に Vectorworks が起動していれば削除しない（cli は、移動したフォルダのスプールと
	// ロックを、動作している Vectorworks が使用しているかもしれない）。標識を残置するため、
	// 終了してからの uninstall が続きから実行される。フォルダを移動してフックも実行したため、
	// 「何もしなかった」の ErrRunning（終了コード 6）ではなく、失敗とする。
	if running(p) {
		return nil, fmt.Errorf("the uninstall hook ran, but %s, so %s stays in %s: quit Vectorworks, then run "+
			"uninstall again (it goes on from the hook)", startedMeanwhile, name, p.oldDir)
	}
	// 構築の場所へ移動してから削除する（削除が途中で停止しても、記録の削除された半分のフォルダを
	// old に残置しない）。再実行の案内（run uninstall <name> again）は discard の文言にあるため、
	// ここでは追加しない。discard の誤りは %v で包み、cmd の区別（ErrRunning・RunsInsideError・
	// 使用方法の誤り）が discard の中まで参照しないようにする（ErrRunning を返すのは開始時の
	// 確認のみ）。
	if err := o.discard(p, name, uninstallCommand(name)); err != nil {
		return nil, fmt.Errorf("%s is out of the folder of plug-ins, but %s is not deleted yet: %v", name, p.oldDir, err)
	}
	return finishUninstall(p, name, outcomeUninstalled, hook), nil
}

// takeOut は <Plug-Ins>/<name>/ を検証して、標識を書き込んでから <Work>/old/<name>/ へ移動し
// （手順 2・3）、その記録を返す。前の uninstall が移動した後で停止していれば、移動せずに old の
// 記録を返し、2 つ目を真とする（手順 4 から継続する）。いずれも無ければ record は
// nil（インストールされていない）。
func (o *Options) takeOut(p places, name string) (*Record, bool, error) {
	// install と同じ pluginFolder で決定する（リンク・ファイルは拒否し、調査できなければ
	// インストールされていないとはみなさない）。
	found, err := pluginFolder(p.dest)
	if err != nil {
		return nil, false, fmt.Errorf("%w; nothing is removed", err)
	}
	// old も、調べられなければ何も変えずに失敗にする（無いとみなすと、old に残った唯一の版を見落とす）。
	oldThere, err := present(p.oldDir)
	if err != nil {
		return nil, false, fmt.Errorf("%w; nothing is removed", checkError(p.oldDir, err, uninstallCommand(name)))
	}
	switch {
	case found:
		// 安全弁: 記録の名前がフォルダ名と一致し、記録の module が実在する場合のみ進行する。
		record, ok := checkedRecord(p.dest, name)
		if !ok {
			reason := "its record is missing, unreadable or for another name, or its module is missing"
			if record != nil && record.Name == name {
				reason = fmt.Sprintf("its module %s is missing; run install to repair it first", record.Module)
			}
			return nil, false, fmt.Errorf("%s is not a plug-in that "+edition.Command+" installed (%s); nothing is removed", p.dest, reason)
		}
		// 実行している CLI がフォルダの中にあれば（インストールした cli の vw2026 で uninstall
		// cli）、アンインストールしない。アンインストールすると CLI は使用しなくなるため、
		// フォルダの外で動作する CLI（初回スクリプトの一時フォルダのもの）でアンインストールする。
		// Windows で実行中の自身を削除できずに残置することもない。
		if _, _, inside := o.runsFrom(p.dest); inside {
			return nil, false, &RunsInsideError{Dir: p.dest, Tag: record.Tag}
		}
		if err := mkdirAll(p.old, 0o700); err != nil {
			return nil, false, err
		}
		// uninstall は <Work>/old/<name>/ を削除しない（遵守事項 9）。install のフックが失敗して
		// Vectorworks が起動した場合の前の版などで、通知せずに削除すると戻せなくなるため。
		// 同じタグ・同じレジストリ・同じ <Plug-Ins> の install が、その install を完了して
		// 整理する。
		if oldThere {
			return nil, false, fmt.Errorf("%s is left by an install that stopped or kept the new version; nothing is removed. "+
				"Run install %s@%s again (from the registry %s, into %s) to finish that install (it cleans this up), "+
				"or delete it, then run uninstall again", p.oldDir, name, record.Tag, record.Registry, p.plugins)
		}
		// 標識を書き込んでからフォルダを移動する（移動した後で停止しても、次の uninstall が続きから
		// 実行できる）。
		if err := writeFile(p.marker, nil, 0o600); err != nil {
			return nil, false, err
		}
		if err := rename(p.dest, p.oldDir); err != nil {
			_ = removeFile(p.marker)
			return nil, false, renameError(p.dest, err, uninstallCommand(name))
		}
		return record, false, nil
	case oldThere:
		// 前の uninstall が、フォルダを移動した後で停止した。標識が無ければ、install が差し替えの
		// 途中で停止したのと区別できない（old が唯一の版かもしれない）。
		// 標識も、確認できなければ何も変更せずに失敗とする（存在しないとみなしても進まないが、理由を取り違えない）。
		markerThere, err := present(p.marker)
		if err != nil {
			return nil, false, fmt.Errorf("%w; nothing is removed", checkError(p.marker, err, uninstallCommand(name)))
		}
		record, ok := checkedRecord(p.oldDir, name)
		if !markerThere || !ok {
			return nil, false, fmt.Errorf("%s was left by an install or uninstall that stopped halfway, and %s is not there; "+
				"it may be the only copy of %s. Move it back to %s or delete it", p.oldDir, p.dest, name, p.dest)
		}
		return record, true, nil
	}
	return nil, false, nil
}

// finishUninstall は、アンインストールを完了した場合（uninstalled）とインストールされていなかった
// 場合（not_installed）の後処理と出力。標識を削除し、退避した CLI の削除を試み、
// 空の <Work> を削除する。
func finishUninstall(p places, name, outcome string, hook json.RawMessage) *UninstallResult {
	_ = removeFile(p.marker)
	left := cleanOldCLIs(p.old)
	removeEmptyWork(p)
	return &UninstallResult{Outcome: outcome, Name: name, Hook: hook, Remaining: installedIn(p.plugins), Left: left}
}

// removeEmptyWork は、<Work> に staging / old の空のディレクトリしか無ければ、<Work> ごと削除する。
// staging / old は空のディレクトリの場合のみ削除し、<Work> は空になった場合のみ削除される
// （removeFile の os.Remove は空でないディレクトリを削除しない）。
func removeEmptyWork(p places) {
	for _, dir := range []string{p.staging, p.old} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = removeFile(dir)
		}
	}
	_ = removeFile(p.work)
}

// installedIn は、<Plug-Ins> の直下で、名前の一致する記録のあるフォルダの名前を、
// 名前の昇順（os.ReadDir の順序）で返す。
func installedIn(plugins string) []string {
	names := []string{}
	entries, err := os.ReadDir(plugins)
	if err != nil {
		return names
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if record, err := readRecord(filepath.Join(plugins, entry.Name())); err == nil && record.Name == entry.Name() {
			names = append(names, entry.Name())
		}
	}
	return names
}
