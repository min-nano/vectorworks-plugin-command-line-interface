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
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// CLIName は cli のプラグインの名前（フォルダ名）。ロックの場所・ほかのプラグインを cli のある <Plug-Ins> に
// だけ入れる確かめ・入った CLI が自分の入った <Plug-Ins> を求める（PluginsDirOf）ためだけに使う。
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

// ErrRunsInside は、uninstall が外すフォルダの中で、走らせている CLI が動いている（入れた cli の vw2026 で
// uninstall cli を走らせた）ので、何もしなかった。フォルダの外の CLI（初回のスクリプト）で外す。
// 返る誤りは *RunsInsideError で、errors.Is で ErrRunsInside と比べられる。
var ErrRunsInside = errors.New("vw2026 runs from the folder it would uninstall")

// RunsInsideError は ErrRunsInside の中身。Tag は入っているタグ（外すのに使う初回のスクリプトと CLI の版）。
type RunsInsideError struct {
	Dir string
	Tag string
}

func (e *RunsInsideError) Error() string {
	return fmt.Sprintf("%v, %s; nothing is removed", ErrRunsInside, e.Dir)
}

// Is は ErrRunsInside と比べさせる。
func (e *RunsInsideError) Is(target error) bool {
	return target == ErrRunsInside
}

// UsageError は使い方の誤り（名前・タグ・指定の形）。
type UsageError struct {
	msg string
}

func (e *UsageError) Error() string {
	return e.msg
}

// Usagef は使い方の誤り（IsUsage が真）を作る。コマンドの側で見つけた誤りも、install・uninstall の中で
// 見つけたものと同じ出力（failed の JSON）と終了コードにするため、パッケージの外にも出す。
func Usagef(format string, args ...any) error {
	return &UsageError{fmt.Sprintf(format, args...)}
}

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
	// 場所は絶対パスだけを受け付ける（今の作業ディレクトリから絶対にすると、それが分からないときに
	// 相対パスのまま名前の変更や削除へ進みうる。消すコードの歯止めに関わるため、組み立てない）。
	switch {
	case o.PluginsDir == "" || o.WorkDir == "":
		return places{}, errors.New("the folder of plug-ins or the work folder is unknown")
	case !filepath.IsAbs(o.PluginsDir):
		return places{}, Usagef("the folder of plug-ins %q is not an absolute path", o.PluginsDir)
	case !filepath.IsAbs(o.WorkDir):
		return places{}, fmt.Errorf("the work folder %q is not an absolute path", o.WorkDir)
	}
	plugins := filepath.Clean(o.PluginsDir)
	work := filepath.Clean(o.WorkDir)
	// 片方がもう片方の中にあれば（初回のスクリプトに <Work>/old を --plugins-dir として渡したなど）、片付けが
	// 入れたばかりのプラグインを消し、付け替えが自分を動かすので断る（消すコードの歯止め。守ること 9）。
	if nested(plugins, work) || nested(work, plugins) {
		return places{}, fmt.Errorf("the folder of plug-ins %s and the work folder %s overlap (one is in the other); "+
			"nothing is changed", plugins, work)
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

// nested は、path が dir と同じか、dir の中にあるか。書いたままのパスと、リンクを解いたパス（resolvedPath。Windows
// では短い名前・大文字と小文字も揃う）の書き方で決める（まだ無い場所も、書き方で見つける。リンクを通して <Work> の
// 下を指す --plugins-dir も、解いた書き方で見つける）。
func nested(path, dir string) bool {
	return lexicallyIn(path, dir) || lexicallyIn(resolvedPath(path), resolvedPath(dir))
}

// lexicallyIn は、書き方で path が dir と同じか、その中か。大文字と小文字を区別しないのが既定の OS（mac・Windows）
// だけ、その違いを除いて比べる（区別するボリュームの、大文字と小文字だけが違う別のフォルダを重ねない。区別しない
// 名前の別の書き方は、同一性の確かめでも見つける）。
func lexicallyIn(path, dir string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		path, dir = strings.ToLower(path), strings.ToLower(dir)
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvedPath は、path のリンクを解いた書き方。まだ無ければ、あるところまでの親を解き、残りをそのまま足す（根まで
// 解けなければ、書いたまま）。
func resolvedPath(path string) string {
	for current, rest := path, ""; ; {
		real, err := filepath.EvalSymlinks(current)
		parent := filepath.Dir(current)
		if err == nil || parent == current {
			return filepath.Join(cmp.Or(real, current), rest)
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}

// stderr は、知らせの行き先（無ければ捨てる）。
func (o *Options) stderr() io.Writer {
	if o.Stderr == nil {
		return io.Discard
	}
	return o.Stderr
}

// lockState は、Vectorworks が居るか（docs/protocol.md「生存の判定」）。<Plug-Ins>/cli/spool/ と、
// <Work>/old/cli/spool/ の両方のロックを見る。cli の付け替え・uninstall の直前に前の版を読み込んだ
// Vectorworks のロックは、フォルダと一緒に old/cli/spool/ へ移る（mac）ので、install・uninstall のどの確かめも
// これだけを使い、道ごとに見る場所を変えない。どちらかのロックを無い以外の理由で調べられなければ（権限・壊れた
// 場所）、動いているかが分からないという誤りも返す（判定そのもの、spool.Open の意味は変えない）。
func lockState(p places) (bool, error) {
	var unknown error
	for _, dir := range []string{filepath.Join(p.plugins, CLIName), filepath.Join(p.old, CLIName)} {
		held, err := spool.LockHeld(filepath.Join(dir, spool.SpoolDirName))
		if held {
			return true, nil
		}
		if err != nil && unknown == nil {
			unknown = fmt.Errorf("cannot tell whether Vectorworks is running, as the lock in %s cannot be read (%v); "+
				"nothing is changed", dir, err)
		}
	}
	return false, unknown
}

// stopped は、何かを変える前の確かめ（始め・同じタグのフックの前・付け替えの前）。Vectorworks が居れば ErrRunning
// （終了コード 6）、ロックを調べられなければその誤り（終了コード 1）を返す。どちらも何も変えていないときに使う。
func stopped(p places) error {
	held, err := lockState(p)
	if held {
		return ErrRunning
	}
	return err
}

// running は、Vectorworks が居るとみなすか。ロックを調べられないときも居るとみなす（付け替えやフックのあとの
// 確かめで、動いているかもしれない Vectorworks の下でフォルダを動かさない）。何も変えていないときは stopped を使う。
func running(p places) bool {
	held, err := lockState(p)
	return held || err != nil
}

// checkCLI は、cli 以外を入れる・外すときに、<Plug-Ins>/cli/ に正しい記録があるかを確かめる。
// cli の無い <Plug-Ins> では Vectorworks が動いているかを判定できないため。
func checkCLI(plugins, name string) error {
	if name == CLIName {
		return nil
	}
	dir := filepath.Join(plugins, CLIName)
	if !validRecord(dir, CLIName) {
		return fmt.Errorf("%s has no cli that vw2026 installed; vw2026 installs and uninstalls a plug-in only in the "+
			"folder of plug-ins that has cli (it tells whether Vectorworks is running from the lock in %s)",
			plugins, filepath.Join(dir, spool.SpoolDirName))
	}
	return nil
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
	if err := checkCLI(p.plugins, name); err != nil {
		return nil, err
	}
	result := &InstallResult{
		Name:       name,
		Requested:  requested,
		Tag:        requested,
		Target:     o.Target,
		PluginsDir: p.plugins,
	}
	// Vectorworks が動いていれば、レジストリ・マニフェストを取る前に答える（ネットワークの誤りや記録の誤りより先に、
	// 終了コード 6。同じタグでも動いていればフックを走らせない）。--check は動いていてもよい。付け替えの直前にも
	// もう一度見る（replace）。
	if !check {
		if err := stopped(p); errors.Is(err, ErrRunning) {
			result.Outcome = OutcomeRunning
			return result, err
		} else if err != nil {
			return nil, err
		}
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
		// 入っている版のフックがまだ通っていなければ（hook_pending）、同じタグでも入れ終えていないので available
		// （同じタグの install でフックを走らせる）。
		if upToDate && !installed.HookPending {
			result.Outcome = OutcomeUpToDate
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
		_ = os.Remove(p.marker)
	}
	if upToDate {
		// 同じタグなら、zip を取らず付け替えもせずに、記録に写したフックだけを走らせる。始めの確かめのあと、レジストリと
		// マニフェストを取る間（時間の上限が無い）に起動した Vectorworks の下でフックを走らせないよう、もう一度見る。
		if err := stopped(p); errors.Is(err, ErrRunning) {
			result.Outcome = OutcomeRunning
			return result, err
		} else if err != nil {
			return nil, err
		}
		hook, err := o.runHook("install", installed.Hooks.Install, p.dest, hookEnv{
			name: name, tag: installed.Tag, previous: previousTag(p, name, installed), target: o.Target,
			pluginDir: p.dest, installDir: p.dest,
		})
		if err != nil {
			return nil, err
		}
		// 新しい版を残した install の続きなら、フックが通ったので、付け替えのあとと同じく印を外して片付ける。
		o.finishInstall(p, name)
		result.Outcome = OutcomeUpToDate
		result.Hook = hook
		return result, nil
	}
	hook, err := o.replace(p, name, source, result.Tag, previousTag(p, name, installed), asset)
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
// ファイルなら、vw2026 が入れたフォルダとみなさずに誤りにする（リンクの先の作業用のコピーなどに触れない）。
// 無い以外の理由で調べられなければ（権限・届かない共有）、その誤りを返す（無いとみなして進まない）。install と
// uninstall はどちらもこれだけで決める（守ること 9 の歯止め）。
func pluginFolder(dest string) (bool, error) {
	info, err := os.Lstat(dest)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case !info.IsDir():
		return false, fmt.Errorf("%s is not a folder that vw2026 installed (not a plain folder); vw2026 does not touch it", dest)
	}
	return true, nil
}

// replace は zip を取って組み立て、プラグインのフォルダを付け替えてフックを走らせる
// （docs/plugin/install-and-update.md「入れ替えの手順」）。失敗したら <Plug-Ins> を元に戻す。
func (o *Options) replace(p places, name, source, tag, previous string, asset registry.Asset) (json.RawMessage, error) {
	for _, dir := range []string{p.staging, p.old} {
		if err := mkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	// 前回の残りの old を片付けてから、組み立ての場所（名前をそのまま組み立てたパスだけ）を消す。
	// settleOld が消すものは組み立ての場所を通るので、この順にすると、消しきれなかったものがあれば
	// removeStaging が失敗し、前の版のファイルを新しい版に混ぜて組み立てない。
	if err := o.settleOld(p, name); err != nil {
		return nil, err
	}
	if err := removeStaging(p); err != nil {
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
	if err := stopped(p); err != nil {
		_ = removeStaging(p)
		return nil, err
	}
	moved, err := o.sidestep(p.dest, p.old)
	if err != nil {
		_ = removeStaging(p)
		return nil, err
	}
	hadOld := exists(p.dest)
	if hadOld {
		if err := rename(p.dest, p.oldDir); err != nil {
			note := moved.restore()
			_ = removeStaging(p)
			return nil, fmt.Errorf("%w%s", renameError(p.dest, err), note)
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
	o.finishInstall(p, name)
	return hook, nil
}

// finishInstall は install のフックが通ったあとの後始末（付け替えのあとも、同じタグでフックだけを走らせたあとも
// これだけを使う）。退避した版を settleOld の規則で片付け（守ること 9 の歯止め。片付けられなくても入れたことは
// 変わらないので、知らせるだけにする。次の install が片付ける）、組み立ての場所の残りと中断した uninstall の印を
// 消し（印を残すと、次の install が付け替えの途中で止まったときに、次の uninstall が唯一残った old/<name>/ を
// 消してしまう）、よけた CLI を消してみる（動いていて消せなければ残す）。
func (o *Options) finishInstall(p places, name string) {
	// フックが通ったので、記録の hook_pending を外す。外せなければ知らせ、退避した版は片付けない（記録はまだ
	// 通っていないと言うので、次の同じタグの install が前の版のタグでフックを走らせ直し、そこで外して片付ける）。
	if err := clearHookPending(p.dest); err != nil {
		fmt.Fprintf(o.stderr(), "vw2026: cannot record that the install hook succeeded (%v); run install with the same "+
			"tag again\n", err)
	} else if err := o.settleOld(p, name); err != nil {
		fmt.Fprintf(o.stderr(), "vw2026: %v\n", err)
	}
	_ = removeStaging(p)
	_ = os.Remove(p.marker)
	cleanOldCLIs(p.old)
}

// undoSwap は付け替えの 2 つ目が失敗したときに、1 つ目とよけた CLI を戻す。
func (o *Options) undoSwap(p places, hadOld bool, moved *movedCLI, cause error) error {
	if hadOld {
		if err := rename(p.oldDir, p.dest); err != nil {
			return fmt.Errorf("cannot put %s into %s (%v), nor put the previous version back (%v); "+
				"move %s back to %s by hand%s", p.stageDir, p.dest, cause, err, p.oldDir, p.dest, moved.where())
		}
	}
	note := moved.restore()
	_ = removeStaging(p)
	return fmt.Errorf("cannot put %s into %s: %w%s", p.stageDir, p.dest, cause, note)
}

// undoHook はフックが失敗したときに前の版へ戻す。戻す前にロックを見て、Vectorworks が起動していれば
// 新しい版を残す（動いているプラグインのロックごと消さない）。
func (o *Options) undoHook(p places, hadOld bool, moved *movedCLI, cause error) error {
	previous := ""
	if hadOld {
		previous = fmt.Sprintf(" (the previous one is in %s)", p.oldDir)
	}
	keep := func(reason string) error {
		// よけた CLI は、前の版のフォルダ（old）の元の場所へ戻す（利用者が前の版を手で戻したときに CLI が欠けず、
		// 後始末がよけた名前のまま消さないように）。戻せなければ、その場所を知らせる。
		note := ""
		if hadOld {
			note = moved.restoreInto(p.dest, p.oldDir)
		}
		// 取った zip はもう要らない（同じタグで走らせ直す install は zip を取らない）。
		_ = os.Remove(p.stageZip)
		return fmt.Errorf("%w; %s, so the new version stays in %s%s: quit Vectorworks, "+
			"then run install with the same tag again (it runs only the hook)%s", cause, reason, p.dest, previous, note)
	}
	if running(p) {
		return keep("Vectorworks started meanwhile (or its lock cannot be read)")
	}
	// 新しい版は名前の変更でよける（消すのはあと。削除が途中で止まると、半分消えたプラグインが
	// Plug-Ins に残るため）。
	if err := rename(p.dest, p.stageDir); err != nil {
		return keep(fmt.Sprintf("it cannot be moved away (%v; Vectorworks may have loaded it)", err))
	}
	if hadOld {
		if err := rename(p.oldDir, p.dest); err != nil {
			return fmt.Errorf("%w; the previous version cannot be put back (%v): move %s back to %s by hand%s",
				cause, err, p.oldDir, p.dest, moved.where())
		}
	}
	note := moved.restore()
	_ = removeStaging(p)
	if note != "" {
		return fmt.Errorf("%w; the previous version is put back%s", cause, note)
	}
	return fmt.Errorf("%w; the previous state is restored", cause)
}

// removeStaging は組み立ての場所（<Work>/staging/<name>/ と <name>.zip）を消す。
func removeStaging(p places) error {
	if err := removeAll(p.stageDir); err != nil {
		return err
	}
	if err := removeFile(p.stageZip); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// settleOld は <Work>/old/<name>/ を片付ける。install のどの道（付け替えの前・付け替えのあと・同じタグで
// フックだけを走らせたあと）もこれだけを使う（docs/plugin/install-and-update.md「<Work>/old/<name>/ の
// 片付け」）。
//
//   - <Plug-Ins>/<name>/ が無ければ、old が唯一の版かもしれないので、何も変えずに失敗にする（印があれば、
//     止まった uninstall の続きを示す）。
//   - 記録の名前が一致し module があれば、消す（守ること 9）。
//   - そうでなければ（module の無いフォルダを入れ直した前の版など。利用者のファイルが入っているかも
//     しれない）、消さずに <Work>/old/<name>.kept-<時刻>/ へ移して知らせる。old/<name>/ に置いたままに
//     すると、次の install・uninstall が通らないため。
func (o *Options) settleOld(p places, name string) error {
	switch {
	case !exists(p.oldDir):
		return nil
	case !exists(p.dest) && exists(p.marker):
		return fmt.Errorf("%s was left by an uninstall that stopped halfway; run uninstall %s again to finish it",
			p.oldDir, name)
	case !exists(p.dest):
		return fmt.Errorf("%s was left by an install or uninstall that stopped halfway; it may be the only copy of %s. "+
			"Move it back to %s or delete it, then run again", p.oldDir, name, p.dest)
	}
	// 動いている Vectorworks が使っているかもしれないものは、消しも移しもしない（フックの間に起動したとき。
	// 始めの確かめでは居なかった）。付け替えの前なら、install は ErrRunning（ロックを調べられなければその誤り）で
	// 何も変えずに終わる。
	if err := stopped(p); err != nil {
		return fmt.Errorf("%w; %s is left because Vectorworks may use it (run install %s with the same tag again "+
			"after quitting it to clean it up)", err, p.oldDir, name)
	}
	if validRecord(p.oldDir, name) {
		return o.discard(p, name)
	}
	kept := filepath.Join(p.old, name+".kept-"+stamp())
	if err := rename(p.oldDir, kept); err != nil {
		return renameError(p.oldDir, err)
	}
	fmt.Fprintf(o.stderr(), "vw2026: the previous folder of %s has no valid record or module, so it is kept in %s "+
		"(it may hold your files); delete it when it is no longer needed\n", name, kept)
	return nil
}

// discard は、確かめた <Work>/old/<name>/ を組み立ての場所（<Work>/staging/<name>/）へ名前の変更で
// 移してから消す。消すのが途中で止まっても（Windows でウイルス対策がファイルを開いているなど）、半分
// 消えたフォルダは old に残らず（記録が先に消えて、次の回が唯一の版と見分けられなくなることがない）、
// 次の install・uninstall が組み立ての場所として消す。移せなければ何も変えずに失敗にする。
func (o *Options) discard(p places, name string) error {
	if err := removeStaging(p); err != nil {
		return err
	}
	// uninstall は空の <Work>/staging を消すので、どの道から来ても、ここで作る。
	if err := mkdirAll(p.staging, 0o700); err != nil {
		return err
	}
	if err := rename(p.oldDir, p.stageDir); err != nil {
		return renameError(p.oldDir, err)
	}
	if err := removeAll(p.stageDir); err != nil {
		fmt.Fprintf(o.stderr(), "vw2026: %s cannot be deleted yet (%v); the next install or uninstall of %s deletes it\n",
			p.stageDir, err, name)
	}
	return nil
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
