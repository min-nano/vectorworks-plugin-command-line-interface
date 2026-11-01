package install

import (
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

// maxDocument はレジストリとマニフェストの大きさの上限（どちらも小さな JSON）。
const maxDocument = 1 << 20

// maxAsset は資産（zip）の大きさの上限（docs/registry.md「zip の形」）。終わらない本文で <Plug-Ins> と
// 同じドライブを埋めないため。テストが縮める。
var maxAsset int64 = 512 << 20

// maxExtracted は zip を展開した大きさの合計の上限（docs/registry.md「zip の形」）。小さな zip が大きく
// 展開されて、<Work> のあるドライブを埋めないため。テストが縮める。
var maxExtracted int64 = 2 << 30

// maxEntries は zip の項目の数の上限（docs/registry.md「zip の形」）。空のファイル・フォルダの多い小さな zip が、
// 大きさの上限に掛からずに、<Work> のあるドライブにファイルを作り続けないため。テストが縮める。
var maxEntries = 100000

// lookup はレジストリを順に引き、名前の項目と、そのレジストリの名前（記録の registry）を返す
// （docs/registry.md「レジストリの順」）。既定のレジストリが先頭で、名前が見つかった時点で止める。
// 見つかる前に取れない・読めないレジストリがあれば、そこで失敗にする（飛ばすと、どの項目が勝つかが
// ネットワークの状態で変わる）。--registry の書き方は、呼ぶ側（install）が先に CheckRegistries で確かめている。
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
	// 引いたレジストリを示す（名前の誤りか、--registry・VW2026_REGISTRY の足りなさかを見分けられるように）。
	return registry.Entry{}, "", fmt.Errorf("%s is in no registry (looked in: %s)", name,
		strings.Join(append([]string{registry.DefaultName}, o.Registries...), " "))
}

// CheckRegistries は、--registry に渡したものすべての書き方を確かめる（checkRegistry。誤りは使い方の誤り）。呼ぶ側
// （cmd）は、場所を求める前にこれで確かめる（CheckRequest と同じ）。
func CheckRegistries(refs []string) error {
	for _, ref := range refs {
		if err := checkRegistry(ref); err != nil {
			return err
		}
	}
	return nil
}

// checkRegistry は --registry に渡したものが https:// か file:// の URL かを確かめる（docs/registry.md
// 「参照の書き方」）。パスはそのままでは受け付けない（file:///… と書く）。記録の registry には、渡した
// 文字列をそのまま残し、比べるのもその文字列どうしだけ。
func checkRegistry(ref string) error {
	if err := registry.CheckRef(ref, false); err != nil {
		return Usagef("the registry: %v (write a path as file:///..., with %%20 for a space)", err)
	}
	return nil
}

// open は参照（https:// か file:// の URL）を開き、大きさ（分からなければ -1）も返す。
func (o *Options) open(ref string) (io.ReadCloser, int64, error) {
	if !registry.IsURL(ref) {
		path, err := registry.FilePath(ref)
		if err != nil {
			return nil, 0, err
		}
		// 手元のファイルはすぐに読めるので、大きさは示さない。
		file, err := os.Open(path)
		return file, -1, err
	}
	client := http.DefaultClient
	if o.Client != nil {
		client = o.Client
	}
	// 転送（releases/latest/download/… など）は追うが、行き先も https:// に限る。
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

// fetch はレジストリかマニフェストを読む。
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

// download は資産を path へ取り、sha256 を照らす。合わなければ取ったものを消して失敗にする。
func (o *Options) download(ref, path, want string) error {
	body, size, err := o.open(ref)
	if err != nil {
		return err
	}
	defer body.Close()
	// 大きさが分かっていて上限を超えるなら、書く前に断る（LimitReader は大きさの分からない応答のため）。
	if size > maxAsset {
		return fmt.Errorf("%s: larger than %d bytes (%d)", ref, maxAsset, size)
	}
	file, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	// 取った量を標準エラーに示す（待つ上限は持たないので、止まっているかを呼ぶ側が見分けられるように）。
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
	// 終わりの行は、大きさの上限・書き込み・sha256 を確かめてから書く（失敗したのに取り終えたとは書かない）。
	progress.finish(err)
	if err != nil {
		_ = removeFile(path)
		return err
	}
	return nil
}
