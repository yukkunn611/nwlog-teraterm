package main

// 画面(HTML)から呼ばれるAPI

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type apiFunc func(body []byte) (any, error)

// ttermpro.exe やインストールフォルダを指定された場合でも、同じ場所の ttpmacro.exe に読み替える
func fixTTPath(p string) string {
	p = strings.Trim(strings.TrimSpace(p), `"`)
	if p == "" || strings.EqualFold(filepath.Base(p), "ttpmacro.exe") {
		return p
	}
	dir := filepath.Dir(p)
	if fi, err := os.Stat(p); err == nil && fi.IsDir() {
		dir = p
	}
	if c := filepath.Join(dir, "ttpmacro.exe"); exists(c) {
		return c
	}
	return p
}

func (a *App) post(mux *http.ServeMux, path string, f apiFunc) {
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		body := make([]byte, 0, 4096)
		buf := make([]byte, 32*1024)
		for {
			n, err := r.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil || len(body) > 32<<20 {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		data, err := f(body)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	})
}

// ログ保存先のフォルダが無くなっている（別PCから持ってきた、フォルダを移動した等）場合は、
// アプリと同じ場所の Log フォルダに付け替える。付け替えたときは元のパスを返す
func (a *App) fixLogDir(p *Profile) string {
	old := p.LogDir
	if fi, err := os.Stat(old); err == nil && fi.IsDir() {
		return ""
	}
	def := a.store.defaultLogDir()
	if strings.EqualFold(filepath.Clean(old), filepath.Clean(def)) {
		return "" // もともと既定の場所。実行時に自動で作られるのでそのまま
	}
	p.LogDir = def
	a.store.saveProfile(*p)
	return old
}

func (a *App) cmdSetMap() map[string]string {
	m := map[string]string{}
	sets, _ := a.store.listCmdSets()
	for _, s := range sets {
		m[s.Name] = s.Content
	}
	return m
}

func (a *App) routes(mux *http.ServeMux) {
	a.updateRoutes(mux)

	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("/api/events", a.handleEvents)

	// 起動時に画面が必要とするものを全部まとめて返す
	a.post(mux, "/api/state", func(body []byte) (any, error) {
		st := a.store.loadSettings()
		names := a.store.listProfiles()
		cur := st.LastProfile
		p, err := a.store.loadProfile(cur)
		if err != nil {
			if len(names) == 0 {
				p = a.store.newProfile("既定")
				a.store.saveProfile(p)
				names = []string{p.Name}
			} else {
				p, err = a.store.loadProfile(names[0])
				if err != nil {
					return nil, err
				}
			}
		}
		logReset := a.fixLogDir(&p)
		sets, _ := a.store.listCmdSets()
		det := detectTeraTerm()
		// 初回はTera Termを自動で探して入れておく
		if st.TtpmacroPath == "" && len(det) > 0 {
			st.TtpmacroPath = det[0].Path
			st.TTVersion = det[0].Version
			a.store.saveSettings(st)
		}
		return map[string]any{
			"appName":         appName,
			"version":         appVersion,
			"author":          appAuthor,
			"repo":            githubRepo,
			"releasePage":     releasePage(),
			"releasesAPI":     releasesAPI(),
			"assetExe":        assetExe,
			"assetSha256":     assetSha256,
			"termsVersion":    termsVersion,
			"devTypeOrder":    devTypeOrder,
			"devTypeDefaults": builtinDevTypes(),
			"settings":        st,
			"profiles":        names,
			"profile":         p,
			"logDirReset":     logReset,
			"cmdSets":         sets,
			"detected":        det,
			"defaultLogDir":   a.store.defaultLogDir(),
			"dataDir":         a.store.dataDir,
			"debugDir":        a.runner.debugRoot,
			"ttpmacroOk":      st.TtpmacroPath != "" && exists(st.TtpmacroPath),
		}, nil
	})

	a.post(mux, "/api/settings/save", func(body []byte) (any, error) {
		var st Settings
		if err := json.Unmarshal(body, &st); err != nil {
			return nil, err
		}
		st.TtpmacroPath = fixTTPath(st.TtpmacroPath)
		// 画面からは送られてこない（送られても無視する）項目は保存済みの値を引き継ぐ
		cur := a.store.loadSettings()
		st.TermsAccepted, st.SkipVersion, st.LastProfile, st.Schema = cur.TermsAccepted, cur.SkipVersion, cur.LastProfile, cur.Schema
		if err := a.store.saveSettings(st); err != nil {
			return nil, err
		}
		st = a.store.loadSettings()
		return map[string]any{"settings": st, "ttpmacroOk": st.TtpmacroPath != "" && exists(st.TtpmacroPath)}, nil
	})

	// 利用規約・免責事項への同意
	a.post(mux, "/api/terms/accept", func(body []byte) (any, error) {
		st := a.store.loadSettings()
		st.TermsAccepted = termsVersion
		return nil, a.store.saveSettings(st)
	})

	a.post(mux, "/api/profile/load", func(body []byte) (any, error) {
		var q struct{ Name string }
		json.Unmarshal(body, &q)
		p, err := a.store.loadProfile(q.Name)
		if err != nil {
			return nil, errors.New("プロファイルを読み込めません: " + err.Error())
		}
		st := a.store.loadSettings()
		st.LastProfile = p.Name
		a.store.saveSettings(st)
		logReset := a.fixLogDir(&p)
		return map[string]any{"profile": p, "logDirReset": logReset}, nil
	})

	a.post(mux, "/api/profile/save", func(body []byte) (any, error) {
		var p Profile
		if err := json.Unmarshal(body, &p); err != nil {
			return nil, err
		}
		return nil, a.store.saveProfile(p)
	})

	a.post(mux, "/api/profile/create", func(body []byte) (any, error) {
		var q struct{ Name, CopyFrom string }
		json.Unmarshal(body, &q)
		q.Name = strings.TrimSpace(q.Name)
		if !validName(q.Name) {
			return nil, errors.New(`プロファイル名に使えない文字（\ / : * ? " < > |）が含まれています`)
		}
		if exists(filepath.Join(a.store.profDir(), q.Name+".json")) {
			return nil, errors.New("同じ名前のプロファイルが既にあります")
		}
		p := a.store.newProfile(q.Name)
		if q.CopyFrom != "" {
			src, err := a.store.loadProfile(q.CopyFrom)
			if err != nil {
				return nil, err
			}
			p = src
			p.Name = q.Name
		}
		if err := a.store.saveProfile(p); err != nil {
			return nil, err
		}
		st := a.store.loadSettings()
		st.LastProfile = p.Name
		a.store.saveSettings(st)
		return map[string]any{"profile": p, "profiles": a.store.listProfiles()}, nil
	})

	a.post(mux, "/api/profile/rename", func(body []byte) (any, error) {
		var q struct{ From, To string }
		json.Unmarshal(body, &q)
		q.To = strings.TrimSpace(q.To)
		if err := a.store.renameProfile(q.From, q.To); err != nil {
			return nil, err
		}
		st := a.store.loadSettings()
		st.LastProfile = q.To
		a.store.saveSettings(st)
		return map[string]any{"profiles": a.store.listProfiles()}, nil
	})

	a.post(mux, "/api/profile/delete", func(body []byte) (any, error) {
		var q struct{ Name string }
		json.Unmarshal(body, &q)
		if len(a.store.listProfiles()) <= 1 {
			return nil, errors.New("最後の1つは削除できません")
		}
		if err := a.store.deleteProfile(q.Name); err != nil {
			return nil, err
		}
		names := a.store.listProfiles()
		p, err := a.store.loadProfile(names[0])
		if err != nil {
			return nil, err
		}
		st := a.store.loadSettings()
		st.LastProfile = p.Name
		a.store.saveSettings(st)
		return map[string]any{"profile": p, "profiles": names}, nil
	})

	a.post(mux, "/api/cmdset/save", func(body []byte) (any, error) {
		var q struct{ Name, Content string }
		json.Unmarshal(body, &q)
		q.Name = strings.TrimSpace(q.Name)
		if q.Name != "" && filepath.Ext(q.Name) == "" {
			q.Name += ".txt"
		}
		if err := a.store.saveCmdSet(q.Name, q.Content); err != nil {
			return nil, err
		}
		sets, _ := a.store.listCmdSets()
		return map[string]any{"cmdSets": sets, "name": q.Name}, nil
	})

	a.post(mux, "/api/cmdset/delete", func(body []byte) (any, error) {
		var q struct{ Name string }
		json.Unmarshal(body, &q)
		if sets, _ := a.store.listCmdSets(); len(sets) <= 1 {
			return nil, errors.New("最後の1つは削除できません")
		}
		if err := a.store.deleteCmdSet(q.Name); err != nil {
			return nil, err
		}
		sets, _ := a.store.listCmdSets()
		return map[string]any{"cmdSets": sets}, nil
	})

	// 取得開始。画面の内容をそのまま保存してから走らせる
	a.post(mux, "/api/run", func(body []byte) (any, error) {
		var q struct {
			Profile Profile
			IDs     []string
		}
		if err := json.Unmarshal(body, &q); err != nil {
			return nil, err
		}
		if err := a.store.saveProfile(q.Profile); err != nil {
			return nil, err
		}
		normalizeProfile(&q.Profile, a.store)
		return nil, a.runner.Start(a.store.loadSettings(), q.Profile, q.IDs, a.cmdSetMap())
	})

	a.post(mux, "/api/abort", func(body []byte) (any, error) {
		a.runner.Abort()
		return nil, nil
	})

	// 疎通チェックだけ単独で行う
	a.post(mux, "/api/precheck", func(body []byte) (any, error) {
		var q struct {
			Profile Profile
			IDs     []string
		}
		if err := json.Unmarshal(body, &q); err != nil {
			return nil, err
		}
		st := a.store.loadSettings()
		want := map[string]bool{}
		for _, id := range q.IDs {
			want[id] = true
		}
		type res struct {
			ID   string `json:"id"`
			OK   bool   `json:"ok"`
			Addr string `json:"addr"`
		}
		var out []res
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 32)
		for _, h := range q.Profile.Hosts {
			if !want[h.ID] || strings.TrimSpace(h.IP) == "" {
				continue
			}
			port := h.Port
			if port <= 0 {
				if h.Proto == "ssh" {
					port = st.SSHPort
				} else {
					port = st.TelnetPort
				}
			}
			addr := net.JoinHostPort(strings.TrimSpace(h.IP), strconv.Itoa(port))
			wg.Add(1)
			go func(id, addr string) {
				defer wg.Done()
				sem <- struct{}{}
				err := tcpCheck(addr, st.PrecheckTimeoutMs)
				<-sem
				mu.Lock()
				out = append(out, res{ID: id, OK: err == nil, Addr: addr})
				mu.Unlock()
			}(h.ID, addr)
		}
		wg.Wait()
		return out, nil
	})

	a.post(mux, "/api/pick", func(body []byte) (any, error) {
		var q struct{ Kind, Current string }
		json.Unmarshal(body, &q)
		if q.Kind == "folder" {
			p, err := pickFolder("ログの保存先フォルダを選んでください")
			return map[string]any{"path": p}, err
		}
		dir := ""
		if q.Current != "" {
			dir = filepath.Dir(q.Current)
		}
		p, err := pickFile("ttpmacro.exe を選んでください（Tera Termのインストールフォルダにあります）", dir)
		if err != nil || p == "" {
			return map[string]any{"path": ""}, err
		}
		p = fixTTPath(p)
		v, det := guessTTVersion(p)
		return map[string]any{"path": p, "version": v, "detail": det}, nil
	})

	// 手入力されたパスの確認と4系/5系の判定
	a.post(mux, "/api/ttinfo", func(body []byte) (any, error) {
		var q struct{ Path string }
		json.Unmarshal(body, &q)
		p := fixTTPath(q.Path)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			return map[string]any{"exists": false, "path": p}, nil
		}
		v, det := guessTTVersion(p)
		return map[string]any{"exists": true, "version": v, "detail": det, "path": p}, nil
	})

	a.post(mux, "/api/open", func(body []byte) (any, error) {
		var q struct {
			Path   string
			Create bool
		}
		json.Unmarshal(body, &q)
		if q.Create {
			os.MkdirAll(q.Path, 0o755)
		}
		if !exists(q.Path) {
			return nil, errors.New("見つかりません: " + q.Path)
		}
		return nil, openPath(q.Path)
	})

	// 結果一覧CSVなどをログ保存先に書き出す（Excelで開けるようBOM付きUTF-8）
	a.post(mux, "/api/save-text", func(body []byte) (any, error) {
		var q struct{ Dir, Name, Content string }
		json.Unmarshal(body, &q)
		if !validName(q.Name) || strings.TrimSpace(q.Dir) == "" {
			return nil, errors.New("保存先が正しくありません")
		}
		if err := os.MkdirAll(q.Dir, 0o755); err != nil {
			return nil, err
		}
		path := filepath.Join(q.Dir, q.Name)
		data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(q.Content)...)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		return map[string]any{"path": path}, nil
	})

	// このアプリのGitHubページだけを既定のブラウザで開く
	a.post(mux, "/api/open-url", func(body []byte) (any, error) {
		var q struct{ URL string }
		json.Unmarshal(body, &q)
		if !strings.HasPrefix(q.URL, "https://github.com/"+githubRepo) {
			return nil, errors.New("開けないURLです")
		}
		return nil, openPath(q.URL)
	})

	// 実際に流すマクロの中身を確認する（パスワードは伏せ字）
	a.post(mux, "/api/ttl-preview", func(body []byte) (any, error) {
		var q struct {
			Profile Profile
			ID      string
		}
		if err := json.Unmarshal(body, &q); err != nil {
			return nil, err
		}
		normalizeProfile(&q.Profile, a.store)
		st := a.store.loadSettings()
		for _, h := range q.Profile.Hosts {
			if h.ID != q.ID {
				continue
			}
			t, err := resolveTarget(st, q.Profile, h, a.cmdSetMap())
			if err != nil {
				return nil, err
			}
			// 表示用なので、組み立てる前にパスワード類を伏せ字へ差し替えておく
			mask := func(v string) string {
				if v == "" {
					return ""
				}
				return "********"
			}
			t.Pass, t.Enable = mask(t.Pass), mask(t.Enable)
			jump := q.Profile.Jump
			jump.Pass = mask(jump.Pass)
			ttl := buildTTL(TTLInput{St: st, Jump: jump, Ident: q.Profile.Ident, LogDir: q.Profile.LogDir, T: t, MarkBase: `%TEMP%\nwlog_tt\h0`})
			return map[string]any{"ttl": ttl}, nil
		}
		return nil, errors.New("ホストが見つかりません")
	})

	// 旧マクロ互換の host_list.csv (Shift_JIS) を作って返す
	mux.HandleFunc("/api/export-csv", func(w http.ResponseWriter, r *http.Request) {
		var p Profile
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(toCP932(hostsToCSV(p)))
	})
}

func csvCell(s string) string {
	// 旧マクロは単純にカンマで割るので、カンマや改行は入れない
	return strings.NewReplacer(",", " ", "\r", " ", "\n", " ").Replace(s)
}

func hostsToCSV(p Profile) string {
	var b strings.Builder
	b.WriteString(";[ ; ] 以降はコメントとみなします,,,,,,,,,\r\n")
	b.WriteString(";接続方式は[telnet] or [ssh]を指定してください,,,,,,,,,\r\n")
	b.WriteString(";,,,,,,,,,\r\n")
	b.WriteString(";[ ; ]で読み飛ばし,;接続方式,;IP,;取得コマンドリスト,;user,;pass,;enable,;仕切り,;備考,;グループ,;機器種別\r\n")
	for _, h := range p.Hosts {
		flag := ""
		if !h.On {
			flag = ";"
		}
		cols := []string{
			flag, h.Proto, strings.TrimSpace(h.IP),
			firstNonEmpty(h.CmdSet, p.Common.CmdSet),
			firstNonEmpty(h.User, p.Common.User),
			firstNonEmpty(h.Pass, p.Common.Pass),
			firstNonEmpty(h.Enable, p.Common.Enable),
			";", csvCell(h.Name), csvCell(h.Group), h.DevType,
		}
		for i := 1; i < 7; i++ {
			cols[i] = csvCell(cols[i])
		}
		b.WriteString(strings.Join(cols, ","))
		b.WriteString("\r\n")
	}
	return b.String()
}
