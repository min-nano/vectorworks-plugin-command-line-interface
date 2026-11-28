package install

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// maxDocument はレジストリとマニフェストの大きさの上限（いずれも小さな JSON）。
const maxDocument = 1 << 20

// maxAsset は資産（zip）の大きさの上限（docs/registry.md「zip の構成」）。終了しない本文で
// <Plug-Ins> と同一のドライブを埋めないため。テストが縮小する。
var maxAsset int64 = 512 << 20

// maxExtracted は zip を展開した大きさの合計の上限（docs/registry.md「zip の構成」）。
// 小さな zip が大きく展開されて、<Work> のあるドライブを埋めないため。テストが縮小する。
var maxExtracted int64 = 2 << 30

// maxEntries は zip の項目の数の上限（docs/registry.md「zip の構成」）。空のファイル・フォルダの
// 多い小さな zip が、大きさの上限に掛からずに、<Work> のあるドライブにファイルを
// 作成し続けないため。テストが縮小する。
var maxEntries = 100000

// lookup はレジストリを順に参照し、名前の項目と、そのレジストリの名前（記録の registry）を返す
// （docs/registry.md「レジストリの優先順位」）。既定のレジストリが先頭で、名前が見つかった時点で
// 停止する。見つかる前に取得できない・読み取れないレジストリがあれば、そこで失敗とする（飛ばすと、
// いずれの項目が優先されるかがネットワークの状態で変わる）。--registry の記法は、
// 呼び出し元（install）が先に CheckRegistries で検証している。
func (o *Options) lookup(name string) (registry.Entry, string, error) {
	def := o.Default
	if def == nil {
		def = registry.Default()
	}
	if entry, ok := def.Plugins[name]; ok {
		return entry, registry.DefaultName, nil
	}
	for _, source := range o.Registries {
		data, err := o.fetch(source)
		if err != nil {
			return registry.Entry{}, "", fmt.Errorf("registry: %w", err)
		}
		reg, err := registry.Parse(data, registry.IsURL(source))
		if err != nil {
			return registry.Entry{}, "", fmt.Errorf("registry %s: %w", source, err)
		}
		if entry, ok := reg.Plugins[name]; ok {
			return entry, source, nil
		}
	}
	// 参照したレジストリを示す（名前の誤りか、--registry・VW2026_REGISTRY の不足かを
	// 区別できるように）。
	return registry.Entry{}, "", fmt.Errorf("%s is in no registry (looked in: %s)", name,
		strings.Join(append([]string{registry.DefaultName}, o.Registries...), " "))
}

// CheckRegistries は、--registry に渡したもの全ての記法を検証する（checkRegistry。
// 誤りは使用方法の誤り）。呼び出し元（cmd）は、場所を導出する前にこれで検証する（CheckRequest と
// 同様）。
func CheckRegistries(refs []string) error {
	for _, ref := range refs {
		if err := checkRegistry(ref); err != nil {
			return err
		}
	}
	return nil
}

// checkRegistry は --registry に渡したものが https:// か file:// の URL かを検証する
// （docs/registry.md「参照の記法」）。パスはそのままでは受け付けない（file:///… と記述する）。
// 記録の registry には、渡した文字列をそのまま保持し、比較するのもその文字列同士のみ。
func checkRegistry(ref string) error {
	if err := registry.CheckRef(ref, false); err != nil {
		return Usagef("the registry: %v (write a path as file:///..., with %%20 for a space)", err)
	}
	return nil
}

// open は参照（https:// か file:// の URL）を開き、大きさ（不明ならば -1）も返す。
func (o *Options) open(ref string) (io.ReadCloser, int64, error) {
	if !registry.IsURL(ref) {
		path, err := registry.FilePath(ref)
		if err != nil {
			return nil, 0, err
		}
		// ローカル環境のファイルは即座に読み取れるため、大きさは示さない。
		file, err := os.Open(path)
		return file, -1, err
	}
	client := cmp.Or(o.Client, http.DefaultClient)
	// 転送（releases/latest/download/… など）には追従するが、転送先も https:// に限定する。
	safe := *client
	safe.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("redirected to %s, which is not https://", req.URL.Redacted())
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	resp, err := safe.Get(ref)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("GET %s: %s", ref, resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}

// fetch はレジストリかマニフェストを読み取る。
func (o *Options) fetch(ref string) ([]byte, error) {
	body, _, err := o.open(ref)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxDocument+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ref, err)
	}
	if len(data) > maxDocument {
		return nil, fmt.Errorf("%s is larger than %d bytes", ref, maxDocument)
	}
	return data, nil
}

// download は資産を path へ取得し、sha256 を照合する。一致しなければ取得したものを削除して
// 失敗とする。
func (o *Options) download(ref, path, want string) error {
	body, size, err := o.open(ref)
	if err != nil {
		return err
	}
	defer body.Close()
	// 大きさが判明していて上限を超えるならば、書き込む前に拒否する（LimitReader は大きさの
	// 不明な応答のため）。
	if size > maxAsset {
		return fmt.Errorf("%s: larger than %d bytes (%d)", ref, maxAsset, size)
	}
	file, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	// 取得した量を標準エラーに示す（待機の上限は設けないため、停止しているかを呼び出し元が
	// 区別できるように）。
	progress := o.startProgress(ref, size)
	written, err := io.Copy(io.MultiWriter(file, hash, progress), io.LimitReader(body, maxAsset+1))
	if err == nil && written > maxAsset {
		err = fmt.Errorf("larger than %d bytes", maxAsset)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		err = fmt.Errorf("%s: %w", ref, err)
	} else if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		err = fmt.Errorf("the sha256 of %s is %s, not %s as the manifest says", ref, got, want)
	}
	// 終了の行は、大きさの上限・書き込み・sha256 を検証してから書き込む（失敗したにもかかわらず
	// 取得を完了したとは書き込まない）。
	progress.finish(err)
	if err != nil {
		_ = removeFile(path)
		return err
	}
	return nil
}
