package main

// NWログ取得マクロ（TeraTerm）
// 開発: Yuki Kayamura
// https://github.com/yukkunn611/nwlog-teraterm  (MIT License)
//
// 画面はHTMLで作り、このEXEが 127.0.0.1 だけで待ち受ける小さなサーバーになって表示する。
// 実際のログ取得は従来どおり Tera Term のマクロ(ttpmacro.exe)にやらせる。

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	appName   = "NWログ取得マクロ（TeraTerm）"
	appAuthor = "Yuki Kayamura"
	// 利用規約の版。内容を変えたらここを更新すると、次の起動時にもう一度同意を求める
	termsVersion = "2026-11-18"
)

// リリース時は GitHub Actions が -ldflags "-X main.appVersion=x.y.z" でタグの番号を入れる
var appVersion = "1.3.1"

//go:embed web
var webFS embed.FS

type App struct {
	store    *Store
	runner   *Runner
	instPath string
	token    string
	host     string // 127.0.0.1:port
	started  time.Time

	hubMu     sync.Mutex
	clients   map[chan []byte]struct{}
	everConn  bool
	lastEmpty time.Time
	dirty     chan struct{}
}

func itoa(n int) string { return strconv.Itoa(n) }

// TTLファイルの文字コード。5系はBOM付きUTF-8、4系はShift_JISで書く
func encodeTTL(ttl, ttVersion string) []byte {
	if ttVersion == "4" {
		return toCP932(ttl)
	}
	return append([]byte{0xEF, 0xBB, 0xBF}, []byte(ttl)...)
}

// UTF-8(BOM有無)かShift_JISかを見分けて文字列にする
func decodeText(b []byte) string {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return string(b[3:])
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return fromCP932(b)
}

type instanceInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func main() {
	afterUpdate(os.Args[1:])
	cleanupOld()
	store, err := newStore()
	if err != nil {
		fatalBox("データフォルダを準備できませんでした。\n" + err.Error())
		os.Exit(1)
	}

	// 二重起動なら、すでに動いている方の画面を出して終わる
	instPath := filepath.Join(store.dataDir, "instance.json")
	if b, err := os.ReadFile(instPath); err == nil {
		var in instanceInfo
		if json.Unmarshal(b, &in) == nil && in.Port > 0 {
			c := http.Client{Timeout: 700 * time.Millisecond}
			url := fmt.Sprintf("http://127.0.0.1:%d/", in.Port)
			if resp, err := c.Get(url + "api/ping?t=" + in.Token); err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					openAppWindow(url + "?t=" + in.Token)
					return
				}
			}
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatalBox("起動できませんでした。\n" + err.Error())
		os.Exit(1)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	tok := make([]byte, 16)
	rand.Read(tok)

	app := &App{
		store:   store,
		token:   hex.EncodeToString(tok),
		host:    fmt.Sprintf("127.0.0.1:%d", port),
		started: time.Now(),
		clients: map[chan []byte]struct{}{},
		dirty:   make(chan struct{}, 1),
	}
	app.lastEmpty = time.Now()
	app.instPath = instPath
	app.runner = newRunner(app.markDirty)
	app.runner.debugRoot = filepath.Join(store.dataDir, "debug")

	ib, _ := json.Marshal(instanceInfo{Port: port, Token: app.token})
	os.WriteFile(instPath, ib, 0o600)
	defer os.Remove(instPath)

	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	app.routes(mux)
	mux.Handle("/", noCache(http.FileServer(http.FS(sub))))

	go app.broadcaster()
	go http.Serve(ln, app.guard(mux))

	url := fmt.Sprintf("http://%s/?t=%s", app.host, app.token)
	if os.Getenv("NWLOG_NOUI") == "" {
		if err := openAppWindow(url); err != nil {
			fatalBox("画面を開けませんでした。ブラウザで次のアドレスを開いてください。\n" + url)
		}
	} else {
		fmt.Println(url)
	}

	// 画面が閉じられたら終了する
	for {
		time.Sleep(500 * time.Millisecond)
		app.hubMu.Lock()
		n := len(app.clients)
		ever := app.everConn
		idle := time.Since(app.lastEmpty)
		app.hubMu.Unlock()
		if n > 0 {
			continue
		}
		if ever && idle > 6*time.Second {
			break
		}
		if !ever && time.Since(app.started) > 120*time.Second {
			break // 画面が一度も開かなかった
		}
	}
	app.runner.Shutdown()
}

// 自動更新で新しい版に切り替えるときの終了処理
func (a *App) quit() {
	a.runner.Shutdown()
	os.Remove(a.instPath)
	os.Exit(0)
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// 外部サイトから勝手に叩かれないよう、Hostヘッダと合言葉(トークン)を確認する
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != a.host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if len(r.URL.Path) >= 5 && r.URL.Path[:5] == "/api/" {
			t := r.Header.Get("X-Token")
			if t == "" {
				t = r.URL.Query().Get("t")
			}
			if t != a.token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) markDirty() {
	select {
	case a.dirty <- struct{}{}:
	default:
	}
}

// ジョブの状態が変わったら、つながっている画面へまとめて送る
func (a *App) broadcaster() {
	for range a.dirty {
		time.Sleep(120 * time.Millisecond) // 細かい更新を束ねる
		select {
		case <-a.dirty:
		default:
		}
		msg := a.jobJSON()
		a.hubMu.Lock()
		for ch := range a.clients {
			select {
			case ch <- msg:
			default: // 詰まっている相手は次の更新で追いつく
			}
		}
		a.hubMu.Unlock()
	}
}

func (a *App) jobJSON() []byte {
	b, _ := json.Marshal(a.runner.snapshot())
	return b
}

func (a *App) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch := make(chan []byte, 8)
	a.hubMu.Lock()
	a.clients[ch] = struct{}{}
	a.everConn = true
	a.hubMu.Unlock()
	defer func() {
		a.hubMu.Lock()
		delete(a.clients, ch)
		if len(a.clients) == 0 {
			a.lastEmpty = time.Now()
		}
		a.hubMu.Unlock()
	}()

	fmt.Fprintf(w, "data: %s\n\n", a.jobJSON())
	fl.Flush()
	keep := time.NewTicker(15 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			fl.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": keep\n\n")
			fl.Flush()
		}
	}
}
