package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
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
	if !registry.ValidName(name) {
		return nil, usageErrorf("invalid name %q (use a-z, 0-9 and -)", name)
	}
	p, err := o.places(name)
	if err != nil {
		return nil, err
	}
	if running(p.plugins) {
		return &UninstallResult{Outcome: OutcomeRunning, Name: name, Remaining: []string{}, Left: []string{}}, ErrRunning
	}
	if err := checkCLI(p.plugins, name); err != nil {
		return nil, err
	}

	var record *Record
	resumed := false
	switch {
	case exists(p.dest):
		// 安全弁: 記録の名前がフォルダ名と一致し、記録の module が実在するときだけ進む。
		record, err = readRecord(p.dest)
		if err != nil || record.Name != name || !record.moduleExists(p.dest) {
			reason := "its record is missing, unreadable or for another name, or its module is missing"
			if err == nil && !record.moduleExists(p.dest) && record.Name == name {
				reason = fmt.Sprintf("its module %s is missing; run install to repair it first", record.Module)
			}
			return nil, fmt.Errorf("%s is not a plug-in that vw2026 installed (%s); nothing is removed", p.dest, reason)
		}
		// 走らせている CLI がフォルダの中にあれば（入れた cli の vw2026 で uninstall cli）、外さない。外すと
		// CLI は使わなくなるので、フォルダの外で動く CLI（初回のスクリプトの一時フォルダのもの）で外す。
		// Windows で動いている自分を消せずに残すこともない。
		if _, _, inside := o.runsFrom(p.dest); inside {
			return nil, &RunsInsideError{Dir: p.dest, Tag: record.Tag}
		}
		if err := mkdirAll(p.old, 0o700); err != nil {
			return nil, err
		}
		// uninstall は <Work>/old/<name>/ を消さない（守ること 9）。install のフックが失敗して Vectorworks が
		// 起動したときの前の版などで、黙って消すと戻せなくなるため。同じタグの install が、その install を
		// 終えて片付ける。
		if exists(p.oldDir) {
			return nil, fmt.Errorf("%s is left by an install that stopped or kept the new version; nothing is removed. "+
				"Run install %s@%s again to finish that install (it cleans this up), or delete it, then run uninstall again",
				p.oldDir, name, record.Tag)
		}
		// 印を書いてからフォルダを移す（移したあとで止まっても、次の uninstall が続きから走れる）。
		if err := writeFile(p.marker, nil, 0o600); err != nil {
			return nil, err
		}
		if err := rename(p.dest, p.oldDir); err != nil {
			_ = os.Remove(p.marker)
			return nil, renameError(p.dest, err)
		}
	case exists(p.oldDir):
		// 前の uninstall が、フォルダを移したあとで止まった。印が無ければ、install が付け替えの
		// 途中で止まったのと見分けられない（old が唯一の版かもしれない）。
		record, err = readRecord(p.oldDir)
		if !exists(p.marker) || err != nil || record.Name != name || !record.moduleExists(p.oldDir) {
			return nil, fmt.Errorf("%s was left by an install or uninstall that stopped halfway, and %s is not there; "+
				"it may be the only copy of %s. Move it back to %s or delete it", p.oldDir, p.dest, name, p.dest)
		}
		resumed = true
	default:
		_ = os.Remove(p.marker)
		// 前の回が消しきれなかった組み立ての場所（discard の残り）も消す。
		_ = removeStaging(p)
		return &UninstallResult{Outcome: OutcomeNotInstall, Name: name, Remaining: installedIn(p.plugins), Left: []string{}}, nil
	}

	hook, err := o.runHook("uninstall", record.Hooks.Uninstall, p.oldDir, hookEnv{
		name: name, tag: record.Tag, target: record.Target, pluginDir: p.oldDir, installDir: p.dest,
	})
	if err != nil {
		// 続きから走った回は戻さない（OS の片付けで一部だけ残ったフォルダかもしれない）。
		if resumed {
			return nil, fmt.Errorf("%w; %s is left as it is (run uninstall again, or delete it and %s)", err, p.oldDir, p.marker)
		}
		// 戻す前にロックを見る（install の undoHook と同じ）。フックの間に起動した Vectorworks の下へ
		// フォルダを戻さず、印を残して、終了してからの uninstall が続きから走れるようにする。
		if running(p.plugins) {
			return nil, fmt.Errorf("%w; Vectorworks started meanwhile, so %s stays in %s: quit Vectorworks, then run "+
				"uninstall again (it goes on from the hook)", err, name, p.oldDir)
		}
		if renameErr := rename(p.oldDir, p.dest); renameErr != nil {
			return nil, fmt.Errorf("%w; %s cannot be put back (%v): move it back to %s by hand", err, p.oldDir, renameErr, p.dest)
		}
		_ = os.Remove(p.marker)
		return nil, fmt.Errorf("%w; %s is put back", err, p.dest)
	}
	// 組み立ての場所へ移してから消す（消すのが途中で止まっても、記録の消えた半分のフォルダを old に残さない）。
	if err := o.discard(p, name); err != nil {
		return nil, fmt.Errorf("%s is out of the folder of plug-ins, but %s cannot be deleted (%v); run uninstall again",
			name, p.oldDir, err)
	}
	_ = os.Remove(p.marker)
	left := cleanOldCLIs(p.old)
	if left == nil {
		left = []string{}
	}
	removeEmptyWork(p)
	return &UninstallResult{
		Outcome:   OutcomeUninstalled,
		Name:      name,
		Hook:      hook,
		Remaining: installedIn(p.plugins),
		Left:      left,
	}, nil
}

// removeEmptyWork は、<Work> に staging / old の空のディレクトリしか無ければ、<Work> ごと消す。
// staging / old は空のディレクトリのときだけ消し、<Work> は空になったときだけ消える（os.Remove は
// 空でないディレクトリを消さない）。
func removeEmptyWork(p places) {
	for _, dir := range []string{p.staging, p.old} {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	_ = os.Remove(p.work)
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

// IsUsage は使い方の誤りか。
func IsUsage(err error) bool {
	var usage *UsageError
	return errors.As(err, &usage)
}
