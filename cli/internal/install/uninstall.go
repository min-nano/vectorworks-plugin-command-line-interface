package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// UninstallResult は uninstall の出力。
type UninstallResult struct {
	Outcome string          `json:"outcome"`
	Name    string          `json:"name"`
	Hook    json.RawMessage `json:"hook,omitempty"`
	// 残った記録のある <Plug-Ins> の直下のフォルダの名前（どの名前でも出す）
	Remaining []string `json:"remaining"`
	// 消せなかったよけた CLI
	Left []string `json:"left"`
}

// Uninstall は <name> を外す（docs/plugin/install-and-update.md「アンインストール」）。レジストリは
// 読まず、記録だけで決める。フックより先にフォルダを <Work>/old/<name>/ へ移し、フックが失敗したら
// 戻す。Vectorworks が動いていれば何もせずに ErrRunning を返す。
func Uninstall(o Options, name string) (*UninstallResult, error) {
	// cmd も同じ CheckName で先に確かめるが、名前から消すパスを組み立てるので、ここでも確かめる（install と同じ）。
	if err := CheckName(name); err != nil {
		return nil, err
	}
	p, err := o.places(name)
	if err != nil {
		return nil, err
	}
	// install と同じ precheck で、cli の記録を Vectorworks が動いているかより先に確かめる。ErrRunning（何も
	// していない）を返すのはここだけで、あとで分かったときは何かを変えたあとなので失敗にする。
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
		// 前の回が消しきれなかった組み立ての場所（discard の残り）・続ける先の無い印・よけた CLI・空の <Work> も、
		// 外したときと同じく片付ける（初回のスクリプトの uninstall が、入っていなくても何も残さないように）。
		_ = removeStaging(p)
		return finishUninstall(p, name, outcomeNotInstall, nil), nil
	}

	hook, err := o.runHook("uninstall", record.Hooks.Uninstall, hookEnv{
		name: name, tag: record.Tag, target: record.Target, pluginDir: p.oldDir, installDir: p.dest,
	})
	if err != nil {
		// 続きから走った回は戻さない（OS の片付けで一部だけ残ったフォルダかもしれない）。
		if resumed {
			return nil, fmt.Errorf("%w; %s is left as it is (run uninstall again, or delete it and %s)", err, p.oldDir, p.marker)
		}
		// 戻す前にロックを見る（install の undoHook と同じ）。フックの間に起動した Vectorworks の下へ
		// フォルダを戻さず、印を残して、終了してからの uninstall が続きから走れるようにする。
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
	// フックの間に Vectorworks が起動していれば消さない（cli は、移したフォルダのスプールとロックを、動いている
	// Vectorworks が使っているかもしれない）。印を残すので、終了してからの uninstall が続きから走る。フォルダを
	// 移してフックも走らせたので、「何もしなかった」の ErrRunning（終了コード 6）ではなく、失敗にする。
	if running(p) {
		return nil, fmt.Errorf("the uninstall hook ran, but %s, so %s stays in %s: quit Vectorworks, then run "+
			"uninstall again (it goes on from the hook)", startedMeanwhile, name, p.oldDir)
	}
	// 組み立ての場所へ移してから消す（消すのが途中で止まっても、記録の消えた半分のフォルダを old に残さない）。
	if err := o.discard(p, name); err != nil {
		return nil, fmt.Errorf("%s is out of the folder of plug-ins, but %s is not deleted yet (run uninstall again to "+
			"delete it): %w", name, p.oldDir, err)
	}
	return finishUninstall(p, name, outcomeUninstalled, hook), nil
}

// takeOut は <Plug-Ins>/<name>/ を確かめて、印を書いてから <Work>/old/<name>/ へ移し（手順 2・3）、その記録を返す。
// 前の uninstall が移したあとで止まっていれば、移さずに old の記録を返し、2 つ目を真にする（手順 4 から続ける）。
// どちらも無ければ record は nil（入っていない）。
func (o *Options) takeOut(p places, name string) (*Record, bool, error) {
	// install と同じ pluginFolder で決める（リンク・ファイルは断り、調べられなければ入っていないとみなさない）。
	found, err := pluginFolder(p.dest)
	if err != nil {
		return nil, false, fmt.Errorf("%w; nothing is removed", err)
	}
	switch {
	case found:
		// 安全弁: 記録の名前がフォルダ名と一致し、記録の module が実在するときだけ進む。
		record, ok := checkedRecord(p.dest, name)
		if !ok {
			reason := "its record is missing, unreadable or for another name, or its module is missing"
			if record != nil && record.Name == name {
				reason = fmt.Sprintf("its module %s is missing; run install to repair it first", record.Module)
			}
			return nil, false, fmt.Errorf("%s is not a plug-in that vw2026 installed (%s); nothing is removed", p.dest, reason)
		}
		// 走らせている CLI がフォルダの中にあれば（入れた cli の vw2026 で uninstall cli）、外さない。外すと
		// CLI は使わなくなるので、フォルダの外で動く CLI（初回のスクリプトの一時フォルダのもの）で外す。
		// Windows で動いている自分を消せずに残すこともない。
		if _, _, inside := o.runsFrom(p.dest); inside {
			return nil, false, &RunsInsideError{Dir: p.dest, Tag: record.Tag}
		}
		if err := mkdirAll(p.old, 0o700); err != nil {
			return nil, false, err
		}
		// uninstall は <Work>/old/<name>/ を消さない（守ること 9）。install のフックが失敗して Vectorworks が
		// 起動したときの前の版などで、黙って消すと戻せなくなるため。同じタグ・同じレジストリ・同じ
		// <Plug-Ins> の install が、その install を終えて片付ける。
		if exists(p.oldDir) {
			return nil, false, fmt.Errorf("%s is left by an install that stopped or kept the new version; nothing is removed. "+
				"Run install %s@%s again (from the registry %s, into %s) to finish that install (it cleans this up), "+
				"or delete it, then run uninstall again", p.oldDir, name, record.Tag, record.Registry, p.plugins)
		}
		// 印を書いてからフォルダを移す（移したあとで止まっても、次の uninstall が続きから走れる）。
		if err := writeFile(p.marker, nil, 0o600); err != nil {
			return nil, false, err
		}
		if err := rename(p.dest, p.oldDir); err != nil {
			_ = removeFile(p.marker)
			return nil, false, renameError(p.dest, err)
		}
		return record, false, nil
	case exists(p.oldDir):
		// 前の uninstall が、フォルダを移したあとで止まった。印が無ければ、install が付け替えの
		// 途中で止まったのと見分けられない（old が唯一の版かもしれない）。
		record, ok := checkedRecord(p.oldDir, name)
		if !exists(p.marker) || !ok {
			return nil, false, fmt.Errorf("%s was left by an install or uninstall that stopped halfway, and %s is not there; "+
				"it may be the only copy of %s. Move it back to %s or delete it", p.oldDir, p.dest, name, p.dest)
		}
		return record, true, nil
	}
	return nil, false, nil
}

// finishUninstall は、外し終えたとき（uninstalled）と入っていなかったとき（not_installed）の後始末と出力。印を消し、
// よけた CLI を消してみて、空の <Work> を消す。
func finishUninstall(p places, name, outcome string, hook json.RawMessage) *UninstallResult {
	_ = removeFile(p.marker)
	left := cleanOldCLIs(p.old)
	removeEmptyWork(p)
	return &UninstallResult{Outcome: outcome, Name: name, Hook: hook, Remaining: installedIn(p.plugins), Left: left}
}

// removeEmptyWork は、<Work> に staging / old の空のディレクトリしか無ければ、<Work> ごと消す。
// staging / old は空のディレクトリのときだけ消し、<Work> は空になったときだけ消える（removeFile の os.Remove は
// 空でないディレクトリを消さない）。
func removeEmptyWork(p places) {
	for _, dir := range []string{p.staging, p.old} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = removeFile(dir)
		}
	}
	_ = removeFile(p.work)
}

// installedIn は、<Plug-Ins> の直下で、名前の一致する記録のあるフォルダの名前を返す。
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
	sort.Strings(names)
	return names
}
