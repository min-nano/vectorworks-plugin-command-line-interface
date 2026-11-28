// Package install はプラグインの配置（vw2026 install / uninstall）を行う
// （docs/plugin/install-and-update.md・docs/registry.md）。全てのプラグインに同一の手順のみを有し、
// cli を特別に扱わない。cli の名前を使用するのは、Vectorworks が動作しているかを確認するロックの
// 場所（<Plug-Ins>/cli/spool）と、他のプラグインを cli と同じ <Plug-Ins> にのみ
// インストールするための検証と、インストールされた CLI が自身のインストール先の <Plug-Ins> を
// 導出すること（PluginsDirOf）のみ。
//
// 場所（<Plug-Ins>・<Work>）と既定のレジストリは Options で受け取る（テストが一時ディレクトリと
// 擬似 HTTP サーバーを渡す。環境変数は増やさない）。
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/edition"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// cliName は cli のプラグインの名前（フォルダ名）。ロックの場所・他のプラグインを cli のある
// <Plug-Ins> にのみインストールする検証・インストールされた CLI が自身のインストール先の
// <Plug-Ins> を導出する（PluginsDirOf）ためのみに使用する。
const cliName = "cli"

// 出力の outcome。
const (
	outcomeInstalled    = "installed"
	outcomeUpToDate     = "up_to_date"
	outcomeAvailable    = "available"
	outcomeRunning      = "vectorworks_running"
	outcomeUninstalled  = "uninstalled"
	outcomeNotInstalled = "not_installed"
)

// Options は配置に必要な外部の環境。
type Options struct {
	// <Plug-Ins>（インストールされた CLI のインストール先・初回スクリプトの --plugins-dir・既定の
	// 場所のいずれか）
	PluginsDir string
	// <Work>（利用者ごとの一時フォルダの下）
	WorkDir string
	// 実行している CLI の実行ファイル（退避するかを決定する。空ならば退避しない）
	Executable string
	// --registry に渡したもの（既定のレジストリの後に、記述した順に参照する）
	Registries []string
	// 既定のレジストリ。nil ならば CLI に埋め込んだもの
	Default *registry.Registry
	// ターゲット（install のみが使用する。本番環境は Target）
	Target string
	// https:// を取得するクライアント。nil ならば既定のもの
	Client *http.Client
	// フックの標準エラー・通知・取得の進捗の出力先。nil ならば破棄する
	Stderr io.Writer
	// Stderr が端末か。端末ならば、取得の進捗を同じ行に書き直す（端末でなければ 1 行ずつ追加する）
	Terminal bool
	// フックの 1 つ目（sh・powershell）を実行する argv の先頭。nil ならば OS ごとに定められたもの。
	// テストが差し替える（sh の無い Windows でも同じフックを実行するため）。
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

// stderr は、通知の出力先（無ければ破棄する）。
func (o *Options) stderr() io.Writer {
	if o.Stderr == nil {
		return io.Discard
	}
	return o.Stderr
}

// CheckName は名前の記法を検証する（誤りは使用方法の誤り）。呼び出し元（cmd）は、
// 場所を導出する前にこれで検証する（場所が不明な場合も、記法の誤りを使用方法の誤りとして
// 応答する）。
func CheckName(name string) error {
	if !registry.ValidName(name) {
		return Usagef("invalid name %q (use a-z, 0-9 and -)", name)
	}
	return nil
}

// CheckRequest は名前とタグの記法を検証する（CheckName と同様に、場所を導出する前に呼び出す）。
func CheckRequest(name, tag string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	if !registry.ValidTag(tag) {
		return Usagef("invalid tag %q (use A-Z, a-z, 0-9, ., _ and -, starting with a letter or a digit)", tag)
	}
	return nil
}

// Install は <name>@<requested> を 1 回インストールする
// （docs/plugin/install-and-update.md「処理の流れ」）。check ならばインストールせずに、
// 新しいビルドがあるかのみを返す。Vectorworks が動作していれば何もせずに ErrRunning を返す（結果の
// outcome も vectorworks_running）。
func Install(o Options, name, requested string, check bool) (*InstallResult, error) {
	result, err := install(o, name, requested, check)
	// いずれの箇所で判明しても、Vectorworks が動作していたら同じ形とする（outcome を経路ごとに
	// 記述しない）。他の誤りでは結果を返さない（呼び出し元は failed を出力する）。
	switch {
	case errors.Is(err, ErrRunning) && result != nil:
		result.Outcome = outcomeRunning
		return result, err
	case err != nil:
		return nil, err
	}
	return result, nil
}

// install は Install の本体。Vectorworks が動作していたら、作成した結果と ErrRunning を返す
// （Install が outcome を決定する）。
func install(o Options, name, requested string, check bool) (*InstallResult, error) {
	// cmd も場所を導出する前に同じ関数（CheckRequest・CheckRegistries）で検証するが、
	// ここでも検証する。名前からは削除するパスを構築するため（遵守事項 9）、パッケージの
	// エントリポイントが自ら制約を有する。同じ関数であるため、文言と規則は乖離しない。
	if err := CheckRequest(name, requested); err != nil {
		return nil, err
	}
	p, err := o.places(name)
	if err != nil {
		return nil, err
	}
	// ネットワークの不要な検証を、レジストリを取得する前に行う（cli の無い <Plug-Ins> ならば、
	// 取得せずに拒否する）。--registry の記法の誤りは、それより先に使用方法の誤りとする。
	if err := CheckRegistries(o.Registries); err != nil {
		return nil, err
	}
	result := &InstallResult{
		Name:       name,
		Requested:  requested,
		Tag:        requested,
		Target:     o.Target,
		PluginsDir: p.plugins,
	}
	// cli の記録と、Vectorworks が動作しているかを、レジストリ・マニフェストを取得する前に応答する
	// （ネットワークの誤りや記録の誤りより先に。同じタグでも動作していればフックを実行しない）。
	// --check は動作していても差し支えない。差し替えの直前にも再度確認する（replace）。
	if err := precheck(p, name, !check); errors.Is(err, ErrRunning) {
		return result, err
	} else if err != nil {
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
	same := func(tag string) bool {
		return installed != nil && installed.Tag == tag && installed.Target == o.Target &&
			installed.moduleExists(p.dest)
	}

	result.Registry = source
	var asset registry.Asset
	upToDate := rule.ByTag() && same(requested)
	// タグで直接指す規則で、インストールされているものと同じタグとターゲットならば、
	// マニフェストも取得しない。
	if !upToDate {
		result.Tag, asset, err = o.manifestAsset(rule, name, requested)
		if err != nil {
			return nil, err
		}
		upToDate = same(result.Tag)
	}
	if check {
		result.Outcome = outcomeAvailable
		// インストールされている版のフックがまだ通過していなければ（hook_pending）、
		// 同じタグでもインストールを完了していないため available（同じタグの install でフックを
		// 実行する）。
		if upToDate && !installed.HookPending {
			result.Outcome = outcomeUpToDate
		}
		return result, nil
	}
	// インストールされている版のフックがまだ通過しておらず（hook_pending）、前の版が old に
	// 残存していれば、差し替えない。差し替えの前の整理が old（フックの通過した最後の版）を削除し、
	// 新しい版のフックが失敗して巻き戻すと、通過していない版のみが残るため。同じタグでフックのみを
	// 実行してインストールを完了する経路（upToDate）は通過させる。Vectorworks が動作していれば、
	// 先に vectorworks_running と応答している。--check は拒否せず available を返す（新しい
	// ビルドがあるかのみを応答する。help に、インストールを完了するまで install が拒否することを
	// 記述する）。
	if installed != nil && installed.HookPending && !upToDate && validRecord(p.oldDir, name) {
		if result.Tag != installed.Tag {
			return nil, fmt.Errorf("%s@%s has not finished installing (its install hook has not succeeded), and the "+
				"previous version, the last that finished, is in %s; run install %s@%s first to finish it, then install the "+
				"new tag (or, to give it up, delete %s and move the previous version back there, then install the new tag)",
				name, installed.Tag, p.oldDir, name, installed.Tag, p.dest)
		}
		// 同じタグでも再インストールとなる（module が欠落した・ターゲットが異なる）場合は、
		// インストールを完了する経路が無いため、利用者が前の版を戻すか削除するかを決定する
		// （いずれかを完了すれば、通常通り再インストールできる）。
		return nil, fmt.Errorf("%s@%s has not finished installing and has to be installed again (its module is missing "+
			"or it is for another target), but the previous version, the last that finished, is in %s; to keep it, "+
			"delete %s and move it back there, otherwise delete it; then run install again",
			name, installed.Tag, p.oldDir, p.dest)
	}
	// <Plug-Ins>/<name>/ が存在するならば、中断した uninstall の標識は以前の回の残存物
	// （uninstall は移動してから停止するため、その場合は <Plug-Ins>/<name>/ が存在しない）。
	// いずれの経路でも先に削除する（残置すると、この後の install が差し替えの途中で停止した場合に、
	// 次の uninstall がそれを中断した uninstall とみなし、唯一残存した old/<name>/ を削除する）。
	// 削除できなければ（ウイルス対策ソフトが開いている場合など）、何も変更せずに失敗とする
	// （標識を残置したまま差し替えへ進まない）。
	if installed != nil {
		if err := removeFile(p.marker); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot delete %s, left by an earlier uninstall (%v); nothing is changed. Close what has "+
				"it open (antivirus software), then run %s again", p.marker, err, installCommand(name, result.Tag))
		}
	}
	if upToDate {
		// 同じタグならば、zip を取得せず差し替えもせずに、記録に複製したフックのみを実行する。
		// 開始時の確認の後、レジストリとマニフェストを取得する間（時間の上限が無い）に起動した
		// Vectorworks の下でフックを実行しないよう、再度確認する。
		if err := stopped(p); err != nil {
			return result, err
		}
		hook, err := o.runHook("install", installed.Hooks.Install, hookEnv{
			name: name, tag: installed.Tag, previous: previousTag(p, name, installed), target: o.Target,
			pluginDir: p.dest, installDir: p.dest,
		})
		if err != nil {
			return nil, err
		}
		// 新しい版を残置した install の続きならば、フックが通過したため、差し替えの後と同様に
		// フラグを除去して整理する。
		o.finishInstall(p, name, installed.Tag, nil, false)
		result.Outcome = outcomeUpToDate
		result.Hook = hook
		return result, nil
	}
	hook, err := o.replace(p, name, source, result.Tag, previousTag(p, name, installed), asset)
	if err != nil {
		return result, err
	}
	result.Outcome = outcomeInstalled
	result.Hook = hook
	return result, nil
}

// manifestAsset は、規則 rule の指すマニフェストを取得し、そのタグと現在のターゲットの asset を返す
// （docs/plugin/install-and-update.md「処理の流れ」の 4）。
func (o *Options) manifestAsset(rule registry.Rule, name, requested string) (string, registry.Asset, error) {
	location := rule.Location(requested)
	data, err := o.fetch(location)
	if err != nil {
		return "", registry.Asset{}, fmt.Errorf("manifest: %w", err)
	}
	manifest, err := registry.ParseManifest(data, registry.IsURL(location))
	if err != nil {
		return "", registry.Asset{}, fmt.Errorf("manifest %s: %w", location, err)
	}
	if rule.ByTag() && manifest.Tag != requested {
		return "", registry.Asset{}, fmt.Errorf("manifest %s has tag %q, not %q", location, manifest.Tag, requested)
	}
	asset, ok := manifest.Targets[o.Target]
	if !ok {
		return "", registry.Asset{}, fmt.Errorf("%s@%s does not support %s", name, manifest.Tag, o.Target)
	}
	return manifest.Tag, asset, nil
}

// inspect はインストール先のフォルダを検証する
// （docs/plugin/install-and-update.md「インストール先のフォルダ」）。無ければ nil。
// 記録が無い・読み取れない・名前か出所が異なれば、何も変更しないように失敗とする。
func inspect(dest, name, source string) (*Record, error) {
	if found, err := pluginFolder(dest); err != nil || !found {
		return nil, err
	}
	record, err := readRecord(dest)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s has no record of "+edition.Command+" (installed by hand or by another installer); "+
			edition.Command+" does not touch it", dest)
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

// previousTag は、フックに渡す前のタグ（VW2026_HOOK_PREVIOUS_TAG）。インストールされていなければ
// 空、インストールされていればそのタグ。但しインストールされている版のフックがまだ
// 通過していなければ（記録の hook_pending。新しい版を残置した install の後）、その版は
// インストールを完了していないため、<Work>/old/<name>/ に残存した前の版のタグとし、
// 無ければ（初めてインストールした場合）空とする（フックが前の版から移行する処理を
// 飛ばさないように。同じタグで完了する回も、別のタグへ更新する回も同様）。
func previousTag(p places, name string, installed *Record) string {
	if installed == nil {
		return ""
	}
	if !installed.HookPending {
		return installed.Tag
	}
	if old, ok := checkedRecord(p.oldDir, name); ok {
		return old.Tag
	}
	return ""
}

// pluginFolder は、<Plug-Ins>/<name> が存在するか。存在してもシンボリックリンク（記録のある
// フォルダを指すものでも）・ジャンクション（Windows。Go は ModeDir と ModeIrregular を返す）・
// ファイルならば、vw2026 がインストールしたフォルダとみなさずに誤りとする（リンク先の作業用の
// 複製などに触れない）。存在しないこと以外の理由で調査できなければ（権限・到達できない共有）、
// その誤りを返す（存在しないとみなして進行しない）。install と uninstall はいずれもこれのみで
// 決定する（遵守事項 9 の制約）。
func pluginFolder(dest string) (bool, error) {
	info, err := lstat(dest)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case info.Mode().Type() != os.ModeDir:
		return false, fmt.Errorf("%s is not a folder that "+edition.Command+" installed (not a plain folder); "+edition.Command+" does not touch it", dest)
	}
	return true, nil
}

// installCommand と uninstallCommand は、利用者に再度実行するよう示すコマンド（edition.Command の
// 後）。示す箇所ごとに表記が食い違わないように 1 か所で構築する。
func installCommand(name, tag string) string {
	return fmt.Sprintf("install %s@%s", name, tag)
}

func uninstallCommand(name string) string {
	return "uninstall " + name
}
