package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/registry"
)

// maxDocument はレジストリとマニフェストの大きさの上限（どちらも小さな JSON）。
const maxDocument = 1 << 20

// lookup はレジストリを順に引き、名前の項目と、そのレジストリの名前（記録の registry）を返す
// （docs/registry.md「レジストリの順」）。既定のレジストリが先頭で、名前が見つかった時点で止める。
// 見つかる前に取れない・読めないレジストリがあれば、そこで失敗にする（飛ばすと、どの項目が勝つかが
// ネットワークの状態で変わる）。
func (o *Options) lookup(name string) (registry.Entry, string, error) {
	def := o.Default
	if def == nil {
		def = registry.Default()
	}
	if entry, ok := def.Plugins[name]; ok {
		return entry, registry.DefaultName, nil
	}
	for _, ref := range o.Registries {
		source, err := RegistrySource(ref)
		if err != nil {
			return registry.Entry{}, "", err
		}
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
	return registry.Entry{}, "", fmt.Errorf("%s is in no registry", name)
}

// RegistrySource は --registry に渡したものを、記録に残す形に整える。https:// の URL はそのまま
// （大文字と小文字も含めて）、パスは今の作業ディレクトリから絶対にして整える（docs/registry.md
// 「入れたときの記録」）。記録の registry と比べるのは、この整えた文字列どうしだけ。
func RegistrySource(ref string) (string, error) {
	switch {
	case ref == "":
		return "", usageErrorf("an empty registry")
	case registry.IsURL(ref):
		return ref, nil
	}
	abs, err := filepath.Abs(ref)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// open は参照（https:// の URL か絶対パス）を開く。
func (o *Options) open(ref string) (io.ReadCloser, error) {
	if !registry.IsURL(ref) {
		if !filepath.IsAbs(ref) {
			return nil, fmt.Errorf("%q is neither an https:// URL nor an absolute path", ref)
		}
		return os.Open(ref)
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
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", ref, resp.Status)
	}
	return resp.Body, nil
}

// fetch はレジストリかマニフェストを読む。
func (o *Options) fetch(ref string) ([]byte, error) {
	body, err := o.open(ref)
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
	body, err := o.open(ref)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := openFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(file, hash), body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return fmt.Errorf("%s: %w", ref, err)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		os.Remove(path)
		return fmt.Errorf("the sha256 of %s is %s, not %s as the manifest says", ref, got, want)
	}
	return nil
}
