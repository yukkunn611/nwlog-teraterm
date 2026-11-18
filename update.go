package main

// 更新の確認と自動更新
// 新しい版は GitHub Releases に置く。確認自体は画面(Edge)側から行い、社内プロキシ経由でも通るようにしている。
// ここはその予備（画面側で確認できなかったとき）と、新しいEXEのダウンロード・差し替えを受け持つ

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	githubRepo  = "yukkunn611/nwlog-teraterm"
	assetExe    = "nwlog-teraterm.exe"        // リリースに添付するEXEの名前（GitHubは日本語のファイル名を崩すため英字にしている）
	assetSha256 = "nwlog-teraterm.exe.sha256" // 上のEXEのSHA-256（16進文字列）
	maxExeSize  = 64 << 20
)

type urlT = url.URL

func releasesAPI() string {
	return "https://api.github.com/repos/" + githubRepo + "/releases?per_page=50"
}

func releasePage() string {
	return "https://github.com/" + githubRepo + "/releases/latest"
}

// 社内環境向けに、環境変数 → Windowsのプロキシ設定(固定指定のみ) の順でプロキシを使う
func httpClient(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = func(r *http.Request) (*urlT, error) {
		if u, err := http.ProxyFromEnvironment(r); u != nil || err != nil {
			return u, err
		}
		return systemProxy(r)
	}
	return &http.Client{Timeout: timeout, Transport: tr}
}

func httpGet(url string, accept string, limit int64) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "nwlog-teraterm/"+appVersion)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := httpClient(5 * time.Minute).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("ファイルが大きすぎます")
	}
	return b, nil
}

var semverRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// "v1.2.3" を比較できる形にする。読めなければ ok=false
func parseVer(s string) (v [3]int, ok bool) {
	m := semverRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return v, false
	}
	for i := 0; i < 3; i++ {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v, true
}

func verLess(a, b [3]int) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// 画面から渡された配布URLが、このアプリのリポジトリのものか確かめる（他所のEXEを入れないため）
func trustedAssetURL(u string) bool {
	return strings.HasPrefix(u, "https://github.com/"+githubRepo+"/releases/download/") ||
		strings.HasPrefix(u, "https://api.github.com/repos/"+githubRepo+"/releases/assets/")
}

// ダウンロードしたEXEを検証して、実行中のEXEと差し替え、新しい方を起動する。
// 実行中のEXEは削除できないが名前の変更はできるので、いったん .old に退避する
func (a *App) applyUpdate(data []byte, wantSha string) error {
	if len(data) < 1024 || data[0] != 'M' || data[1] != 'Z' {
		return errors.New("ダウンロードしたファイルがWindowsの実行ファイルではありません")
	}
	wantSha = strings.ToLower(strings.TrimSpace(strings.Fields(wantSha + " x")[0]))
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	if len(wantSha) != 64 {
		return errors.New("チェックサム(SHA-256)を取得できなかったため、安全のため更新を中止しました")
	}
	if got != wantSha {
		return errors.New("ダウンロードしたファイルのチェックサムが一致しません（破損の可能性）")
	}
	if a.runner.isRunning() {
		return errors.New("ログ取得の実行中は更新できません。終わってからもう一度お試しください")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	newPath := exe + ".new"
	oldPath := exe + ".old"
	if err := os.WriteFile(newPath, data, 0o755); err != nil {
		return errors.New("新しいEXEを書き込めません（フォルダの書き込み権限を確認してください）: " + err.Error())
	}
	os.Remove(oldPath)
	if err := os.Rename(exe, oldPath); err != nil {
		os.Remove(newPath)
		return errors.New("いまのEXEを退避できません: " + err.Error())
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(oldPath, exe) // 元に戻す
		os.Remove(newPath)
		return errors.New("新しいEXEに置き換えられません: " + err.Error())
	}
	cmd := exec.Command(exe, "--after-update", strconv.Itoa(os.Getpid()))
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		os.Rename(exe, newPath)
		os.Rename(oldPath, exe)
		os.Remove(newPath)
		return errors.New("新しいバージョンを起動できません: " + err.Error())
	}
	// 画面に結果を返してから終了する
	go func() {
		time.Sleep(1500 * time.Millisecond)
		a.quit()
	}()
	return nil
}

// 更新後に起動されたときの後始末。古いプロセスが終わるのを待ってから、退避した .old を消す
func afterUpdate(args []string) {
	for i, s := range args {
		if s != "--after-update" || i+1 >= len(args) {
			continue
		}
		pid, _ := strconv.Atoi(args[i+1])
		deadline := time.Now().Add(15 * time.Second)
		for pid > 0 && processAlive(pid) && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
		}
	}
	cleanupOld()
}

func cleanupOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	if !exists(old) {
		return
	}
	go func() {
		for i := 0; i < 20; i++ {
			if os.Remove(old) == nil || !exists(old) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
}

func (a *App) updateRoutes(mux *http.ServeMux) {
	// 画面側で GitHub に届かなかったときの予備
	a.post(mux, "/api/update/check", func(body []byte) (any, error) {
		b, err := httpGet(releasesAPI(), "application/vnd.github+json", 4<<20)
		if err != nil {
			return nil, errors.New("更新を確認できませんでした: " + err.Error())
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return v, nil
	})

	// 「このバージョンは再度表示しない」
	a.post(mux, "/api/update/skip", func(body []byte) (any, error) {
		var q struct{ Version string }
		json.Unmarshal(body, &q)
		st := a.store.loadSettings()
		st.SkipVersion = q.Version
		return nil, a.store.saveSettings(st)
	})

	// アプリ側でダウンロードして差し替える
	a.post(mux, "/api/update/apply", func(body []byte) (any, error) {
		var q struct {
			ExeURL string
			ShaURL string
		}
		json.Unmarshal(body, &q)
		if !trustedAssetURL(q.ExeURL) || !trustedAssetURL(q.ShaURL) {
			return nil, errors.New("配布元のURLが正しくありません")
		}
		sha, err := httpGet(q.ShaURL, "application/octet-stream", 4096)
		if err != nil {
			return nil, errors.New("チェックサムを取得できませんでした: " + err.Error())
		}
		data, err := httpGet(q.ExeURL, "application/octet-stream", maxExeSize)
		if err != nil {
			return nil, errors.New("ダウンロードできませんでした: " + err.Error())
		}
		return nil, a.applyUpdate(data, string(sha))
	})

	// 画面(ブラウザ)側でダウンロードできた場合は、その中身を受け取って差し替える
	mux.HandleFunc("/api/update/apply-bytes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		reply := func(err error) {
			if err != nil {
				json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, maxExeSize+1))
		if err != nil {
			reply(err)
			return
		}
		if len(data) > maxExeSize {
			reply(errors.New("ファイルが大きすぎます"))
			return
		}
		reply(a.applyUpdate(data, r.Header.Get("X-Sha256")))
	})
}
