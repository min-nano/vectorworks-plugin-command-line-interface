// Package install はプラグインの配置（vw2026 install / uninstall）を行う
// （docs/plugin/install-and-update.md・docs/registry.md）。どのプラグインにも同じ手順だけを持ち、
// cli を特別に扱わない。cli の名前を使うのは、Vectorworks が動いているかを見るロックの場所
// （<Plug-Ins>/cli/spool）と、ほかのプラグインを cli と同じ <Plug-Ins> にだけ入れるための確かめだけ。
//
// 場所（<Plug-Ins>・<Work>）と既定のレジストリは Options で受け取る（テストが一時ディレクトリと
// 偽の HTTP サーバーを渡す。環境変数は増やさない）。
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// CLIName は cli のプラグインの名前（フォルダ名）。ロックの場所を決めるためだけに使う。
const CLIName = "cli"

// 出力の outcome。
const (
	OutcomeInstalled   = "installed"
	OutcomeUpToDate    = "up_to_date"
	OutcomeAvailable   = "available"
	OutcomeRunning     = "vectorworks_running"
	OutcomeUninstalled = "uninstalled"
	OutcomeNotInstall  = "not_installed"
)

// ErrRunning は、ロックを掴む Vectorworks が居るので何もしなかった（専用の終了コード）。
var ErrRunning = errors.New("Vectorworks is running: quit it, then run again")

// UsageError は使い方の誤り（名前・タグ・指定の形）。
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string {
	return e.msg
}

func usageErrorf(format string, args ...any) error {
	return &UsageError{fmt.Sprintf(format, args...)}
}

// Options は配置に要る外の世界。
type Options struct {
	// <Plug-Ins>（--plugins-dir か既定の場所）
	PluginsDir string
	// <Work>（利用者ごとの一時フォルダの下）
	WorkDir string
	// 走らせている CLI の実行ファイル（よけるかを決める。空ならよけない）
	Executable string
	// --registry に渡したもの（既定のレジストリの後ろに、書いた順に引く）
	Registries []string
	// 既定のレジストリ。nil なら CLI に埋め込んだもの
	Default *registry.Registry
	// ターゲット（install だけが使う）。空なら計算機から決める（Target）
	Target string
	// https:// を取るクライアント。nil なら既定のもの
	Client *http.Client
	// フックの標準エラーの行き先。nil なら捨てる
	Stderr io.Writer
	// フックの 1 つ目（sh・powershell）を走らせる argv の頭。nil なら OS の決まったもの。テストが
	// 差し替える（sh の無い Windows でも同じフックを走らせるため）。
	Interpreters map[string][]string
}

// InstallResult は install の出力。
type InstallResult struct {
	Outcome    string          `json:"outcome"`
	Name       string          `json:"name"`
	Requested  string          `json:"requested,omitempty"`
	Tag        string          `json:"tag,omitempty"`
	Target     string          `json:"target,omitempty"`
	Registry   string          `json:"registry,omitempty"`
	PluginsDir string          `json:"plugins_dir,omitempty"`
	Hook       json.RawMessage `json:"hook,omitempty"`
}

// places は、Options の場所から決まるパス。
type places struct {
	plugins  string
	dest     string
	work     string
	staging  string
	stageDir string
	stageZip string
	old      string
	oldDir   string
	marker   string
}

func (o *Options) places(name string) (places, error) {
	if o.PluginsDir == "" || o.WorkDir == "" {
		return places{}, errors.New("the folder of plug-ins or the work folder is unknown")
	}
	plugins, err := filepath.Abs(o.PluginsDir)
	if err != nil {
		return places{}, err
	}
	work, err := filepath.Abs(o.WorkDir)
	if err != nil {
		return places{}, err
	}
	staging := filepath.Join(work, "staging")
	old := filepath.Join(work, "old")
	return places{
		plugins:  plugins,
		dest:     filepath.Join(plugins, name),
		work:     work,
		staging:  staging,
		stageDir: filepath.Join(staging, name),
		stageZip: filepath.Join(staging, name+".zip"),
		old:      old,
		oldDir:   filepath.Join(old, name),
		marker:   filepath.Join(old, name+".uninstalling"),
	}, nil
}

// running は、入れる先の <Plug-Ins>/cli/spool/ のロックを掴む Vectorworks が居るか
// （docs/protocol.md「生存の判定」）。
func running(plugins string) bool {
	return spool.Open(filepath.Join(plugins, CLIName, spool.SpoolDirName)).Running
}

// checkCLI は、cli 以外を入れる・外すときに、<Plug-Ins>/cli/ に正しい記録があるかを確かめる。
// cli の無い <Plug-Ins> では Vectorworks が動いているかを判定できないため。
func checkCLI(plugins, name string) error {
	if name == CLIName {
		return nil
	}
	dir := filepath.Join(plugins, CLIName)
	if record, err := readRecord(dir); err != nil || record.Name != CLIName || !record.moduleExists(dir) {
		return fmt.Errorf("%s has no cli that vw2026 installed; vw2026 installs and uninstalls a plug-in only in the "+
			"folder of plug-ins that has cli (it tells whether Vectorworks is running from the lock in %s)",
			plugins, filepath.Join(dir, spool.SpoolDirName))
	}
	return nil
}

// Install は <name>@<requested> を 1 回入れる（docs/plugin/install-and-update.md「流れ」）。
// check なら入れずに、新しいビルドがあるかだけを返す。Vectorworks が動いていれば何もせずに
// ErrRunning を返す（結果の outcome も vectorworks_running）。
func Install(o Options, name, requested string, check bool) (*InstallResult, error) {
	if !registry.ValidName(name) {
		return nil, usageErrorf("invalid name %q (use a-z, 0-9 and -)", name)
	}
	if !registry.ValidTag(requested) {
		return nil, usageErrorf("invalid tag %q (use A-Z, a-z, 0-9, ., _ and -, starting with a letter or a digit)", requested)
	}
	if o.Target == "" {
		target, err := Target()
		if err != nil {
			return nil, err
		}
		o.Target = target
	}
	p, err := o.places(name)
	if err != nil {
		return nil, err
	}
	entry, source, err := o.lookup(name)
	if err != nil {
		return nil, err
	}
	rule, ok := entry.Select(requested)
	if !ok {
		return nil, fmt.Errorf("no rule for %s in registry %s matches %q", name, source, requested)
	}
	installed, err := inspect(p.dest, name, source)
	if err != nil {
		return nil, err
	}
	if err := checkCLI(p.plugins, name); err != nil {
		return nil, err
	}
	same := func(tag string) bool {
		return installed != nil && installed.Tag == tag && installed.Target == o.Target &&
			installed.moduleExists(p.dest)
	}

	result := &InstallResult{
		Name:       name,
		Requested:  requested,
		Tag:        requested,
		Target:     o.Target,
		Registry:   source,
		PluginsDir: p.plugins,
	}
	var asset registry.Asset
	upToDate := rule.ByTag() && same(requested)
	// タグで直接指す規則で、入っているものと同じタグとターゲットなら、マニフェストも取らない。
	if !upToDate {
		location := rule.Location(requested)
		data, err := o.fetch(location)
		if err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}
		manifest, err := registry.ParseManifest(data, registry.IsURL(location))
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", location, err)
		}
		if rule.ByTag() && manifest.Tag != requested {
			return nil, fmt.Errorf("manifest %s has tag %q, not %q", location, manifest.Tag, requested)
		}
		asset, ok = manifest.Targets[o.Target]
		if !ok {
			return nil, fmt.Errorf("%s@%s does not support %s", name, manifest.Tag, o.Target)
		}
		result.Tag = manifest.Tag
		upToDate = same(manifest.Tag)
	}
	if check {
		result.Outcome = OutcomeAvailable
		if upToDate {
			result.Outcome = OutcomeUpToDate
		}
		return result, nil
	}
	if running(p.plugins) {
		result.Outcome = OutcomeRunning
		return result, ErrRunning
	}
	if upToDate {
		// 同じタグなら、zip を取らず付け替えもせずに、記録に写したフックだけを走らせる。
		hook, err := o.runHook("install", installed.Hooks.Install, p.dest, hookEnv{
			name: name, tag: installed.Tag, previous: installed.Tag, target: o.Target,
			pluginDir: p.dest, installDir: p.dest,
		})
		if err != nil {
			return nil, err
		}
		result.Outcome = OutcomeUpToDate
		result.Hook = hook
		return result, nil
	}
	previous := ""
	if installed != nil {
		previous = installed.Tag
	}
	hook, err := o.replace(p, name, source, result.Tag, previous, asset)
	if err != nil {
		if errors.Is(err, ErrRunning) {
			result.Outcome = OutcomeRunning
			return result, err
		}
		return nil, err
	}
	result.Outcome = OutcomeInstalled
	result.Hook = hook
	return result, nil
}

// inspect は入れる先のフォルダを確かめる（docs/plugin/install-and-update.md「入れる先のフォルダ」）。
// 無ければ nil。記録が無い・読めない・名前か出どころが違えば、何も変えないように失敗にする。
func inspect(dest, name, source string) (*Record, error) {
	info, err := os.Lstat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder that vw2026 installed; vw2026 does not touch it", dest)
	}
	record, err := readRecord(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s has no record of vw2026 (installed by hand or by another installer); "+
			"vw2026 does not touch it", dest)
	}
	if err != nil {
		return nil, fmt.Errorf("the record in %s cannot be read (%v); decide what to do with the folder", dest, err)
	}
	if record.Name != name {
		return nil, fmt.Errorf("the record in %s is for %q, not %q; decide what to do with the folder", dest, record.Name, name)
	}
	if record.Registry != source {
		return nil, fmt.Errorf("%s was installed from registry %s, not %s; uninstall it before installing from another registry",
			dest, record.Registry, source)
	}
	return record, nil
}

// replace は zip を取って組み立て、プラグインのフォルダを付け替えてフックを走らせる
// （docs/plugin/install-and-update.md「入れ替えの手順」）。失敗したら <Plug-Ins> を元に戻す。
func (o *Options) replace(p places, name, source, tag, previous string, asset registry.Asset) (json.RawMessage, error) {
	for _, dir := range []string{p.staging, p.old} {
		if err := mkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	// 前回の残りの組み立てを消す（名前をそのまま組み立てたパスだけ）。
	if err := removeStaging(p); err != nil {
		return nil, err
	}
	if err := removeLeftover(p, name); err != nil {
		return nil, err
	}

	// 組み立て。失敗したら組み立てたものを消す。
	record, err := o.assemble(p, name, source, tag, asset)
	if err != nil {
		_ = removeStaging(p)
		return nil, err
	}

	// 付け替えの直前にロックをもう一度見る（zip を取って組み立てる間に起動した Vectorworks を
	// 見落とさないため）。
	if running(p.plugins) {
		_ = removeStaging(p)
		return nil, ErrRunning
	}
	moved, err := o.sidestep(p.dest, p.old)
	if err != nil {
		_ = removeStaging(p)
		return nil, err
	}
	hadOld := exists(p.dest)
	if hadOld {
		if err := rename(p.dest, p.oldDir); err != nil {
			moved.restore()
			_ = removeStaging(p)
			return nil, renameError(p.dest, err)
		}
	}
	if err := mkdirAll(p.plugins, 0o755); err != nil {
		return nil, o.undoSwap(p, hadOld, moved, err)
	}
	if err := rename(p.stageDir, p.dest); err != nil {
		return nil, o.undoSwap(p, hadOld, moved, err)
	}

	hook, err := o.runHook("install", record.Hooks.Install, p.dest, hookEnv{
		name: name, tag: tag, previous: previous, target: o.Target, pluginDir: p.dest, installDir: p.dest,
	})
	if err != nil {
		return nil, o.undoHook(p, hadOld, moved, err)
	}

	// 後始末。付け替えに成功したので、中断した uninstall の印も消す（残すと、次の install が付け替えの
	// 途中で止まったときに、次の uninstall が唯一残った old/<name>/ を消してしまう）。
	_ = os.Remove(p.stageZip)
	_ = os.RemoveAll(p.oldDir)
	_ = os.Remove(p.marker)
	cleanOldCLIs(p.old)
	return hook, nil
}

// undoSwap は付け替えの 2 つ目が失敗したときに、1 つ目とよけた CLI を戻す。
func (o *Options) undoSwap(p places, hadOld bool, moved *movedCLI, cause error) error {
	if hadOld {
		if err := rename(p.oldDir, p.dest); err != nil {
			return fmt.Errorf("cannot put %s into %s (%v), nor put the previous version back (%v); "+
				"move %s back to %s by hand", p.stageDir, p.dest, cause, err, p.oldDir, p.dest)
		}
	}
	moved.restore()
	_ = removeStaging(p)
	return fmt.Errorf("cannot put %s into %s: %w", p.stageDir, p.dest, cause)
}

// undoHook はフックが失敗したときに前の版へ戻す。戻す前にロックを見て、Vectorworks が起動していれば
// 新しい版を残す（動いているプラグインのロックごと消さない）。
func (o *Options) undoHook(p places, hadOld bool, moved *movedCLI, cause error) error {
	keep := func(reason string) error {
		return fmt.Errorf("%w; %s, so the new version stays in %s (the previous one is in %s): quit Vectorworks, "+
			"then run install with the same tag again (it runs only the hook)", cause, reason, p.dest, p.oldDir)
	}
	if running(p.plugins) {
		return keep("Vectorworks started meanwhile")
	}
	// 新しい版は名前の変更でよける（消すのはあと。削除が途中で止まると、半分消えたプラグインが
	// Plug-Ins に残るため）。
	if err := rename(p.dest, p.stageDir); err != nil {
		return keep(fmt.Sprintf("it cannot be moved away (%v; Vectorworks may have loaded it)", err))
	}
	if hadOld {
		if err := rename(p.oldDir, p.dest); err != nil {
			return fmt.Errorf("%w; the previous version cannot be put back (%v): move %s back to %s by hand",
				cause, err, p.oldDir, p.dest)
		}
	}
	moved.restore()
	_ = removeStaging(p)
	return fmt.Errorf("%w; the previous state is restored", cause)
}

// removeStaging は組み立ての場所（<Work>/staging/<name>/ と <name>.zip）を消す。
func removeStaging(p places) error {
	if err := removeAll(p.stageDir); err != nil {
		return err
	}
	if err := os.Remove(p.stageZip); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// removeLeftover は前の install / uninstall が付け替えの途中で止まって残した <Work>/old/<name>/ を
// 片付ける。<Plug-Ins>/<name>/ もあり、記録が正しければ退避の残りとして消す。それ以外は唯一の版
// かもしれないので、何も変えずに失敗にする。
func removeLeftover(p places, name string) error {
	if !exists(p.oldDir) {
		return nil
	}
	if exists(p.dest) && validRecord(p.oldDir, name) {
		return os.RemoveAll(p.oldDir)
	}
	return fmt.Errorf("%s was left by an install or uninstall that stopped halfway; it may be the only copy of %s. "+
		"Move it back to %s or delete it, then run again", p.oldDir, name, p.dest)
}

// renameError は付け替えの 1 つ目（<Plug-Ins>/<name> を移す）が失敗したときの文言。Windows では、
// フォルダの中のものを開いているプロセスが居ると名前を変えられない。どれかは見分けられない。
func renameError(dir string, err error) error {
	return fmt.Errorf("cannot rename %s (%w). Close what has files in it open (Explorer, antivirus software, "+
		"Vectorworks), move the current directory of terminals and other programs out of it, then run again", dir, err)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
