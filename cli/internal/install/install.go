// Package install はプラグインの配置（vw2026 install / uninstall）を行う
// （docs/plugin/install-and-update.md・docs/registry.md）。どのプラグインにも同じ手順だけを持ち、
// cli を特別に扱わない。cli の名前を使うのは、Vectorworks が動いているかを見るロックの場所
// （<Plug-Ins>/cli/spool）と、ほかのプラグインを cli と同じ <Plug-Ins> にだけ入れるための確かめと、入った CLI が
// 自分の入った <Plug-Ins> を求めること（PluginsDirOf）だけ。
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

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// cliName は cli のプラグインの名前（フォルダ名）。ロックの場所・ほかのプラグインを cli のある <Plug-Ins> に
// だけ入れる確かめ・入った CLI が自分の入った <Plug-Ins> を求める（PluginsDirOf）ためだけに使う。
const cliName = "cli"

// 出力の outcome。
const (
	outcomeInstalled   = "installed"
	outcomeUpToDate    = "up_to_date"
	outcomeAvailable   = "available"
	outcomeRunning     = "vectorworks_running"
	outcomeUninstalled = "uninstalled"
	outcomeNotInstall  = "not_installed"
)

// Options は配置に要る外の世界。
type Options struct {
	// <Plug-Ins>（入った CLI の入った場所・初回のスクリプトの --plugins-dir・既定の場所のどれか）
	PluginsDir string
	// <Work>（利用者ごとの一時フォルダの下）
	WorkDir string
	// 走らせている CLI の実行ファイル（よけるかを決める。空ならよけない）
	Executable string
	// --registry に渡したもの（既定のレジストリの後ろに、書いた順に引く）
	Registries []string
	// 既定のレジストリ。nil なら CLI に埋め込んだもの
	Default *registry.Registry
	// ターゲット（install だけが使う。本番は Target）
	Target string
	// https:// を取るクライアント。nil なら既定のもの
	Client *http.Client
	// フックの標準エラー・知らせ・取得の進み具合の行き先。nil なら捨てる
	Stderr io.Writer
	// Stderr が端末か。端末なら、取得の進み具合を同じ行に書き直す（端末でなければ 1 行ずつ足す）
	Terminal bool
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

// stderr は、知らせの行き先（無ければ捨てる）。
func (o *Options) stderr() io.Writer {
	if o.Stderr == nil {
		return io.Discard
	}
	return o.Stderr
}

// CheckName は名前の書き方を確かめる（誤りは使い方の誤り）。呼ぶ側（cmd）は、場所を求める前にこれで確かめる
// （場所が分からないときも、書き方の誤りを使い方の誤りとして答える）。
func CheckName(name string) error {
	if !registry.ValidName(name) {
		return Usagef("invalid name %q (use a-z, 0-9 and -)", name)
	}
	return nil
}

// CheckRequest は名前とタグの書き方を確かめる（CheckName と同じく、場所を求める前に呼ぶ）。
func CheckRequest(name, tag string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	if !registry.ValidTag(tag) {
		return Usagef("invalid tag %q (use A-Z, a-z, 0-9, ., _ and -, starting with a letter or a digit)", tag)
	}
	return nil
}

// Install は <name>@<requested> を 1 回入れる（docs/plugin/install-and-update.md「流れ」）。
// check なら入れずに、新しいビルドがあるかだけを返す。Vectorworks が動いていれば何もせずに
// ErrRunning を返す（結果の outcome も vectorworks_running）。
func Install(o Options, name, requested string, check bool) (*InstallResult, error) {
	result, err := install(o, name, requested, check)
	// どこで分かっても、Vectorworks が動いていたら同じ形にする（outcome を道ごとに書かない）。ほかの誤りでは結果を
	// 返さない（呼ぶ側は failed を出す）。
	switch {
	case errors.Is(err, ErrRunning) && result != nil:
		result.Outcome = outcomeRunning
		return result, err
	case err != nil:
		return nil, err
	}
	return result, nil
}

// install は Install の本体。Vectorworks が動いていたら、作った結果と ErrRunning を返す（Install が outcome を
// 決める）。
func install(o Options, name, requested string, check bool) (*InstallResult, error) {
	// cmd も場所を求める前に同じ関数（CheckRequest・CheckRegistries）で確かめるが、ここでも確かめる。名前からは消す
	// パスを組み立てるので（守ること 9）、パッケージの入り口が自分で歯止めを持つ。同じ関数なので、文言と規則はずれない。
	if err := CheckRequest(name, requested); err != nil {
		return nil, err
	}
	p, err := o.places(name)
	if err != nil {
		return nil, err
	}
	// ネットワークの要らない確かめを、レジストリを取る前にする（cli の無い <Plug-Ins> なら、取りに行かずに断る）。
	// --registry の書き方の誤りは、それより先に使い方の誤りにする。
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
	// cli の記録と、Vectorworks が動いているかを、レジストリ・マニフェストを取る前に答える（ネットワークの誤りや
	// 記録の誤りより先に。同じタグでも動いていればフックを走らせない）。--check は動いていてもよい。付け替えの直前にも
	// もう一度見る（replace）。
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
	// タグで直接指す規則で、入っているものと同じタグとターゲットなら、マニフェストも取らない。
	if !upToDate {
		result.Tag, asset, err = o.manifestAsset(rule, name, requested)
		if err != nil {
			return nil, err
		}
		upToDate = same(result.Tag)
	}
	if check {
		result.Outcome = outcomeAvailable
		// 入っている版のフックがまだ通っていなければ（hook_pending）、同じタグでも入れ終えていないので available
		// （同じタグの install でフックを走らせる）。
		if upToDate && !installed.HookPending {
			result.Outcome = outcomeUpToDate
		}
		return result, nil
	}
	// 入っている版のフックがまだ通っておらず（hook_pending）、前の版が old に残っていれば、付け替えない。付け替えの
	// 前の片付けが old（フックの通った最後の版）を消し、新しい版のフックが失敗して戻すと、通っていない版だけが残る
	// ため。同じタグでフックだけを走らせて入れ終える道（upToDate）は通す。Vectorworks が動いていれば、先に
	// vectorworks_running と答えている。--check は断らず available を返す（新しいビルドがあるかだけを答える。help に、
	// 入れ終えるまで install が断ることを書く）。
	if installed != nil && installed.HookPending && !upToDate && validRecord(p.oldDir, name) {
		if result.Tag != installed.Tag {
			return nil, fmt.Errorf("%s@%s has not finished installing (its install hook has not succeeded), and the "+
				"previous version, the last that finished, is in %s; run install %s@%s first to finish it, then install the "+
				"new tag (or, to give it up, delete %s and move the previous version back there, then install the new tag)",
				name, installed.Tag, p.oldDir, name, installed.Tag, p.dest)
		}
		// 同じタグでも入れ直しになる（module が欠けた・ターゲットが違う）ときは、入れ終える道が無いので、利用者が
		// 前の版を戻すか消すかを決める（どちらも済めば、ふつうに入れ直せる）。
		return nil, fmt.Errorf("%s@%s has not finished installing and has to be installed again (its module is missing "+
			"or it is for another target), but the previous version, the last that finished, is in %s; to keep it, "+
			"delete %s and move it back there, otherwise delete it; then run install again",
			name, installed.Tag, p.oldDir, p.dest)
	}
	// <Plug-Ins>/<name>/ があるなら、中断した uninstall の印は前の回の残り（uninstall は移してから止まる
	// ので、そのときは <Plug-Ins>/<name>/ が無い）。どの道でも先に消す（残すと、このあとの install が付け替えの
	// 途中で止まったときに、次の uninstall がそれを中断した uninstall とみなし、唯一残った old/<name>/ を消す）。
	if installed != nil {
		_ = removeFile(p.marker)
	}
	if upToDate {
		// 同じタグなら、zip を取らず付け替えもせずに、記録に写したフックだけを走らせる。始めの確かめのあと、レジストリと
		// マニフェストを取る間（時間の上限が無い）に起動した Vectorworks の下でフックを走らせないよう、もう一度見る。
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
		// 新しい版を残した install の続きなら、フックが通ったので、付け替えのあとと同じく印を外して片付ける。
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

// manifestAsset は、規則 rule の指すマニフェストを取り、そのタグと今のターゲットの asset を返す
// （docs/plugin/install-and-update.md「流れ」の 4）。
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

// inspect は入れる先のフォルダを確かめる（docs/plugin/install-and-update.md「入れる先のフォルダ」）。
// 無ければ nil。記録が無い・読めない・名前か出どころが違えば、何も変えないように失敗にする。
func inspect(dest, name, source string) (*Record, error) {
	if found, err := pluginFolder(dest); err != nil || !found {
		return nil, err
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

// previousTag は、フックに渡す前のタグ（VW2026_HOOK_PREVIOUS_TAG）。入っていなければ空、入っていればそのタグ。
// ただし入っている版のフックがまだ通っていなければ（記録の hook_pending。新しい版を残した install のあと）、
// その版は入れ終えていないので、<Work>/old/<name>/ に残った前の版のタグにし、無ければ（最初に入れたとき）空に
// する（フックが前の版から移す処理を飛ばさないように。同じタグで終える回も、別のタグへ更新する回も同じ）。
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

// pluginFolder は、<Plug-Ins>/<name> が在るか。在ってもシンボリックリンク（記録のあるフォルダを指すものでも）・
// ジャンクション（Windows。Go は ModeDir と ModeIrregular を返す）・ファイルなら、vw2026 が入れたフォルダとみなさずに誤りにする（リンクの先の作業用のコピーなどに触れない）。
// 無い以外の理由で調べられなければ（権限・届かない共有）、その誤りを返す（無いとみなして進まない）。install と
// uninstall はどちらもこれだけで決める（守ること 9 の歯止め）。
func pluginFolder(dest string) (bool, error) {
	info, err := os.Lstat(dest)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case info.Mode().Type() != os.ModeDir:
		return false, fmt.Errorf("%s is not a folder that vw2026 installed (not a plain folder); vw2026 does not touch it", dest)
	}
	return true, nil
}
