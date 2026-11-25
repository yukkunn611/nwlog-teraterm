package main

// 取得ジョブの実行エンジン
// ホストごとにTTLを書き出して ttpmacro.exe を起動し、マクロが作るマーカーファイルで進み具合を追いかける

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// デバッグモード用のログ。nilのままでも呼べるようにしてある
type dbgLog struct {
	mu  sync.Mutex
	f   *os.File
	dir string
}

func (d *dbgLog) logf(format string, a ...any) {
	if d == nil || d.f == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	fmt.Fprintf(d.f, "%s  %s\r\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, a...))
}

func (d *dbgLog) close() {
	if d != nil && d.f != nil {
		d.f.Close()
	}
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>| `, r) {
			return '_'
		}
		return r
	}, s)
}

type HostState struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Status   string `json:"status"` // queued / check / running / ok / warn / error / aborted
	Stage    string `json:"stage"`
	CmdDone  int    `json:"cmdDone"`
	CmdTotal int    `json:"cmdTotal"`
	Hostname string `json:"hostname"`
	LogFile  string `json:"logFile"`
	Message  string `json:"message"`
	Started  int64  `json:"started"` // unix ms
	Ended    int64  `json:"ended"`
}

type JobState struct {
	Seq      int          `json:"seq"`
	Running  bool         `json:"running"`
	Aborting bool         `json:"aborting"`
	Started  int64        `json:"started"`
	Ended    int64        `json:"ended"`
	LogDir   string       `json:"logDir"`
	Parallel int          `json:"parallel"`
	Relay    bool         `json:"relay"`
	DebugDir string       `json:"debugDir"` // デバッグモードで実行したときのログ置き場
	Hosts    []*HostState `json:"hosts"`
}

type Runner struct {
	mu      sync.Mutex
	job     *JobState
	seq     int
	abortCh chan struct{}
	procs   map[string]*procHandle
	tmpRoot string
	onDirty func()

	debugRoot string
}

func newRunner(onDirty func()) *Runner {
	r := &Runner{
		procs:   map[string]*procHandle{},
		tmpRoot: filepath.Join(os.TempDir(), "nwlog_tt"),
		onDirty: onDirty,
	}
	// 前回異常終了したときの残骸（パスワード入りTTL）を消しておく
	os.RemoveAll(r.tmpRoot)
	return r
}

func (r *Runner) snapshot() *JobState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.job == nil {
		return nil
	}
	cp := *r.job
	cp.Hosts = make([]*HostState, len(r.job.Hosts))
	for i, h := range r.job.Hosts {
		c := *h
		cp.Hosts[i] = &c
	}
	return &cp
}

func (r *Runner) isRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job != nil && r.job.Running
}

func (r *Runner) update(h *HostState, f func(h *HostState)) {
	r.mu.Lock()
	f(h)
	r.mu.Unlock()
	r.onDirty()
}

// 共通認証・既定ポート・個別タイムアウトを解決して1台ぶんの実行情報を作る
func resolveTarget(st Settings, p Profile, h Host, sets map[string]string) (Target, error) {
	t := Target{Host: h, DT: effectiveDevType(st, h)}
	t.User = firstNonEmpty(h.User, p.Common.User)
	t.Pass = firstNonEmpty(h.Pass, p.Common.Pass)
	t.Enable = firstNonEmpty(h.Enable, p.Common.Enable)
	t.Port = h.Port
	if t.Port <= 0 {
		if h.Proto == "ssh" {
			t.Port = st.SSHPort
		} else {
			t.Port = st.TelnetPort
		}
	}
	t.LoginTimeout = st.LoginTimeout
	if h.LoginTimeout > 0 {
		t.LoginTimeout = h.LoginTimeout
	}
	t.CmdTimeout = st.CmdTimeout
	if h.CmdTimeout > 0 {
		t.CmdTimeout = h.CmdTimeout
	}
	ip := strings.TrimSpace(h.IP)
	if ip == "" {
		return t, errors.New("IPアドレスが空です")
	}
	if strings.ContainsAny(ip, " \t'\";/\\") {
		return t, errors.New("IPアドレスに使えない文字が含まれています")
	}
	t.Host.IP = ip
	if h.Proto == "ssh" && (t.User == "" || t.Pass == "") {
		return t, errors.New("SSHにはユーザ名とパスワードが必要です")
	}
	if h.Proto != "ssh" && t.Pass == "" {
		return t, errors.New("ログインパスワードが空です")
	}
	name := firstNonEmpty(h.CmdSet, p.Common.CmdSet)
	if name == "" {
		return t, errors.New("コマンドセットが選ばれていません")
	}
	content, ok := sets[name]
	if !ok {
		return t, fmt.Errorf("コマンドセット「%s」が見つかりません", name)
	}
	t.Cmds = splitCommands(content)
	if len(t.Cmds) == 0 {
		return t, fmt.Errorf("コマンドセット「%s」が空です", name)
	}
	return t, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ジョブ開始。ここでは検証だけして、実処理はゴルーチンに任せる
func (r *Runner) Start(st Settings, p Profile, ids []string, sets map[string]string) error {
	r.mu.Lock()
	if r.job != nil && r.job.Running {
		r.mu.Unlock()
		return errors.New("すでに実行中です")
	}
	r.mu.Unlock()

	if st.TtpmacroPath == "" {
		return errors.New("ttpmacro.exe の場所が未設定です。詳細設定で指定してください")
	}
	if fi, err := os.Stat(st.TtpmacroPath); err != nil || fi.IsDir() {
		return errors.New("ttpmacro.exe が見つかりません: " + st.TtpmacroPath)
	}
	if strings.ContainsAny(p.Ident, `\/:*?"<>|`) {
		return errors.New(`識別子にファイル名に使えない文字（\ / : * ? " < > |）が含まれています`)
	}
	if err := os.MkdirAll(p.LogDir, 0o755); err != nil {
		return errors.New("ログ保存先を作成できません: " + err.Error())
	}
	if p.Jump.Enabled {
		if strings.TrimSpace(p.Jump.IP) == "" {
			return errors.New("踏み台のIPアドレスが未入力です")
		}
		if p.Jump.Proto == "ssh" && (p.Jump.User == "" || p.Jump.Pass == "") {
			return errors.New("踏み台(SSH)のユーザ名とパスワードを入力してください")
		}
	}

	byID := map[string]Host{}
	for _, h := range p.Hosts {
		byID[h.ID] = h
	}
	type item struct {
		hs  *HostState
		tgt Target
		err error
	}
	var items []*item
	for _, id := range ids {
		h, ok := byID[id]
		if !ok {
			continue
		}
		t, err := resolveTarget(st, p, h, sets)
		hs := &HostState{ID: h.ID, Name: h.Name, IP: strings.TrimSpace(h.IP), Status: "queued", Stage: "待機中", CmdTotal: len(t.Cmds)}
		items = append(items, &item{hs: hs, tgt: t, err: err})
	}
	if len(items) == 0 {
		return errors.New("取得対象のホストがありません（チェックを入れてください）")
	}

	// 踏み台を使うときは必ず1台ずつ
	parallel := p.Concurrency
	if p.Mode == "serial" || p.Jump.Enabled {
		parallel = 1
	}
	if parallel > len(items) {
		parallel = len(items)
	}

	r.mu.Lock()
	r.seq++
	job := &JobState{Seq: r.seq, Running: true, Started: nowMs(), LogDir: p.LogDir, Parallel: parallel, Relay: p.Jump.Enabled}
	for _, it := range items {
		job.Hosts = append(job.Hosts, it.hs)
	}
	r.job = job
	r.abortCh = make(chan struct{})
	abortCh := r.abortCh
	runDir := filepath.Join(r.tmpRoot, fmt.Sprintf("run%d_%d", os.Getpid(), r.seq))
	r.mu.Unlock()

	if err := os.MkdirAll(runDir, 0o700); err != nil {
		r.mu.Lock()
		job.Running = false
		r.mu.Unlock()
		return errors.New("作業フォルダを作成できません: " + err.Error())
	}

	// デバッグモード：この回の調査用フォルダを作り、環境と設定を書き出しておく
	var dbg *dbgLog
	if st.Debug && r.debugRoot != "" {
		dir := filepath.Join(r.debugRoot, "run_"+time.Now().Format("20060102-150405"))
		if os.MkdirAll(dir, 0o755) == nil {
			if f, err := os.Create(filepath.Join(dir, "app.log")); err == nil {
				f.Write([]byte{0xEF, 0xBB, 0xBF})
				dbg = &dbgLog{f: f, dir: dir}
				r.mu.Lock()
				job.DebugDir = dir
				r.mu.Unlock()
				_, ttDetail := guessTTVersion(st.TtpmacroPath)
				sj, _ := json.Marshal(st)
				dbg.logf("%s v%s  (%s/%s)", appName, appVersion, runtime.GOOS, runtime.GOARCH)
				dbg.logf("ttpmacro: %s  [%s]  マクロ文字コード=%s系向け", st.TtpmacroPath, ttDetail, st.TTVersion)
				dbg.logf("設定: %s", sj)
				dbg.logf("取得方式: mode=%s 同時接続数=%d 実際の並列数=%d 順番を譲るタイミング=%s 踏み台=%v 対象=%d台", p.Mode, p.Concurrency, parallel, st.StartupWait, p.Jump.Enabled, len(items))
				dbg.logf("ログ保存先: %s / 作業フォルダ: %s", p.LogDir, runDir)
				for i, it := range items {
					e := ""
					if it.err != nil {
						e = "  設定エラー: " + it.err.Error()
					}
					dbg.logf("  h%d  %s  %s  %s:%d  コマンド%d個%s", i, it.hs.Name, it.tgt.Host.Proto, it.hs.IP, it.tgt.Port, len(it.tgt.Cmds), e)
				}
			}
		}
	}
	r.onDirty()

	go func() {
		queue := make(chan int, len(items))
		for i := range items {
			queue <- i
		}
		close(queue)

		gate := newLaunchGate() // Tera Termの起動をリストの上から順に1台ずつ行うための順番待ち
		var wg sync.WaitGroup
		for slot := 0; slot < parallel; slot++ {
			wg.Add(1)
			go func(slot int) {
				defer wg.Done()
				for i := range queue {
					it := items[i]
					select {
					case <-abortCh:
						r.update(it.hs, func(h *HostState) {
							h.Status = "aborted"
							h.Stage = "中断"
							h.Message = "開始前に中断されました"
						})
						gate.pass(i)
						continue
					default:
					}
					if it.err != nil {
						gate.pass(i)
						r.update(it.hs, func(h *HostState) {
							h.Status = "error"
							h.Stage = "設定エラー"
							h.Message = it.err.Error()
							h.Started, h.Ended = nowMs(), nowMs()
						})
						continue
					}
					r.runOne(st, p, it.hs, it.tgt, runDir, i, slot, parallel, abortCh, gate, dbg)
					gate.pass(i) // 起動まで行かずに終わった場合でも、後ろを待たせたままにしない
				}
			}(slot)
		}
		wg.Wait()
		os.RemoveAll(runDir)
		dbg.logf("ジョブ終了")
		dbg.close()
		r.mu.Lock()
		job.Running = false
		job.Aborting = false
		job.Ended = nowMs()
		r.mu.Unlock()
		r.onDirty()
	}()
	return nil
}

func nowMs() int64 { return time.Now().UnixMilli() }

// 起動の順番待ち。番号の若い順に1台ずつ通し、通過(pass)したら次の番号を起こす
type launchGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	next   int
	passed map[int]bool
}

func newLaunchGate() *launchGate {
	g := &launchGate{passed: map[int]bool{}}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *launchGate) wait(seq int) {
	g.mu.Lock()
	for g.next != seq {
		g.cond.Wait()
	}
	g.mu.Unlock()
}

func (g *launchGate) pass(seq int) {
	g.mu.Lock()
	if !g.passed[seq] {
		g.passed[seq] = true
		for g.passed[g.next] {
			g.next++
		}
		g.cond.Broadcast()
	}
	g.mu.Unlock()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (r *Runner) runOne(st Settings, p Profile, hs *HostState, t Target, runDir string, idx, slot, parallel int, abortCh chan struct{}, gate *launchGate, dbg *dbgLog) {
	r.update(hs, func(h *HostState) { h.Started = nowMs() })
	tag := fmt.Sprintf("[h%d %s]", idx, t.Host.IP)
	fail := func(stage, msg string) {
		dbg.logf("%s 失敗: %s / %s", tag, stage, msg)
		r.update(hs, func(h *HostState) { h.Status = "error"; h.Stage = stage; h.Message = msg; h.Ended = nowMs() })
	}
	aborted := func() {
		r.update(hs, func(h *HostState) {
			h.Status = "aborted"
			h.Stage = "中断"
			h.Message = "開始前に中断されました"
			h.Ended = nowMs()
		})
	}

	// 事前疎通チェック。踏み台経由のときは手元PCから対象に届かないのが普通なので踏み台だけ見る
	if st.Precheck {
		r.update(hs, func(h *HostState) { h.Status = "check"; h.Stage = "疎通確認中" })
		addr := net.JoinHostPort(t.Host.IP, strconv.Itoa(t.Port))
		what := "対象機器"
		if p.Jump.Enabled {
			jp := p.Jump.Port
			if jp <= 0 {
				if p.Jump.Proto == "ssh" {
					jp = st.SSHPort
				} else {
					jp = st.TelnetPort
				}
			}
			addr = net.JoinHostPort(strings.TrimSpace(p.Jump.IP), strconv.Itoa(jp))
			what = "踏み台"
		}
		t0 := time.Now()
		if err := tcpCheck(addr, st.PrecheckTimeoutMs); err != nil {
			dbg.logf("%s 疎通チェックNG %s (%dms): %v", tag, addr, time.Since(t0).Milliseconds(), err)
			fail("到達不可", fmt.Sprintf("%s(%s)に届きません", what, addr))
			return
		}
		dbg.logf("%s 疎通チェックOK %s (%dms)", tag, addr, time.Since(t0).Milliseconds())
	}

	base := filepath.Join(runDir, fmt.Sprintf("h%d", idx))
	ttlPath := base + ".ttl"
	in := TTLInput{St: st, Jump: p.Jump, Ident: p.Ident, LogDir: p.LogDir, T: t, MarkBase: base, Slot: slot}
	// 並列のときだけ順番待ちを入れる。1台ずつのときは同時にリンクすることがないので不要
	handshake := ""
	if parallel > 1 {
		handshake = st.StartupWait
	}
	in.Handshake = handshake
	if dbg != nil {
		stem := filepath.Join(dbg.dir, fmt.Sprintf("h%d_%s", idx, safeName(t.Host.IP)))
		in.TraceFile = stem + "_trace.txt"
		// 人に渡しても困らないよう、パスワードを伏せ字にしたマクロの写しを残す
		mask := func(v string) string {
			if v == "" {
				return ""
			}
			return "********"
		}
		min := in
		min.T.Pass, min.T.Enable, min.Jump.Pass = mask(t.Pass), mask(t.Enable), mask(p.Jump.Pass)
		os.WriteFile(stem+"_macro.ttl", append([]byte{0xEF, 0xBB, 0xBF}, []byte(buildTTL(min))...), 0o644)
	}
	if err := os.WriteFile(ttlPath, encodeTTL(buildTTL(in), st.TTVersion), 0o600); err != nil {
		fail("準備失敗", "マクロを書き出せません: "+err.Error())
		return
	}
	defer func() {
		// パスワードが入っているので必ず消す
		os.Remove(ttlPath)
		for _, f := range globMarkers(base) {
			os.Remove(f)
		}
	}()

	// Tera Termを何台も同時にマクロとリンク(DDE)させると、呼びかけ同士が詰まって
	// connect から戻ってこなくなる。そこで ttpmacro.exe 自体は全部すぐに起動しておき、
	// リンクの瞬間だけリストの上から1台ずつ通す。マクロは合図ファイル(.go)を待ってからリンクし、
	// リンクが済んだら(.lk)、または接続まで済んだら(.s2)次の1台に合図を出す。
	// 機器への接続・ログイン・ログ取得は他の台と同時に進む。
	released := false
	launched := false
	release := func() {
		if handshake == "" || released {
			return
		}
		released = true
		wait := 0
		// 実際に起動したときだけ間隔をあける（中断時に待ち行列がすぐ捌けるように）
		if launched {
			wait = st.LaunchIntervalMs
		}
		go func() {
			time.Sleep(time.Duration(wait) * time.Millisecond)
			gate.pass(idx)
		}()
	}
	defer release()
	select {
	case <-abortCh:
		aborted()
		return
	default:
	}
	args := []string{"/I", ttlPath}
	if st.WindowMode == "hidden" {
		args = []string{"/V", ttlPath}
	}
	tq := time.Now()
	cmd := exec.Command(st.TtpmacroPath, args...)
	cmd.Dir = filepath.Dir(st.TtpmacroPath)
	if err := cmd.Start(); err != nil {
		fail("起動失敗", "ttpmacro.exe を起動できません: "+err.Error())
		return
	}
	launched = true
	ph := attachProc(cmd)
	r.mu.Lock()
	r.procs[hs.ID] = ph
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.procs, hs.ID)
		r.mu.Unlock()
		ph.close()
	}()
	dbg.logf("%s 起動 pid=%d 枠=%d 起動にかかった時間=%dms ジョブ登録=%v 順番待ち=%q  %s %s", tag, cmd.Process.Pid, slot, time.Since(tq).Milliseconds(), ph.inJob(), handshake, st.TtpmacroPath, strings.Join(args, " "))

	// 自分の番が来たら知らせてもらう（ゲートの中で待つと中断や異常終了に気付けないので別に待つ）
	myTurn := make(chan struct{})
	if handshake != "" {
		r.update(hs, func(h *HostState) { h.Status = "running"; h.Stage = "接続の順番待ち" })
		go func() { gate.wait(idx); close(myTurn) }()
	} else {
		close(myTurn)
	}
	gotTurn := false
	var turnAt time.Time
	// 前面にないウィンドウでも速度を落とされないようにしておく（Tera Term本体は接続後にもう一度かける）
	unthrottleTree(cmd.Process.Pid)

	if handshake == "" {
		r.update(hs, func(h *HostState) { h.Status = "running"; h.Stage = "Tera Term起動中" })
	}

	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()

	started := time.Now()
	turnAt = started
	linked := false
	connected := false
	gotHost := false
	sawLogin := false
	lastCmd := 0
	killedWhy := ""
	var lastSpeed time.Time
	logPath := ""
	lastSize := int64(-1)
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()

	// 止まっているときの様子（どのプロセスが残り、どんなウィンドウ/ダイアログが出ているか）を控える
	diagNote := ""
	snapshot := func(why string) {
		rep, dialogs := diagSnapshot(cmd.Process.Pid)
		if rep != "" {
			dbg.logf("%s %s時点のプロセス/ウィンドウ:\r\n%s", tag, why, rep)
		}
		if len(dialogs) > 0 {
			diagNote = "　表示されていたダイアログ: " + strings.Join(dialogs, " ／ ")
		}
	}
	kill := func(why, label string) {
		killedWhy = why
		if label != "" {
			snapshot(label)
		}
		ph.kill()
		<-exited
	}

loop:
	for {
		select {
		case <-exited:
			break loop
		case <-abortCh:
			kill("abort", "")
			break loop
		case <-myTurn:
			myTurn = nil
			gotTurn = true
			turnAt = time.Now()
			if handshake != "" {
				if err := os.WriteFile(base+".go", nil, 0o644); err != nil {
					dbg.logf("%s 合図ファイルを作れません: %v", tag, err)
				}
				dbg.logf("%s 順番が来たので合図(.go) 起動から%dms", tag, time.Since(started).Milliseconds())
				r.update(hs, func(h *HostState) { h.Stage = "Tera Term起動中" })
			}
		case <-tick.C:
			// マーカーを見て進捗を更新
			if gotTurn && !linked && handshake == "link" && exists(base+".lk") {
				linked = true
				dbg.logf("%s リンク完了(.lk) 合図から%dms", tag, time.Since(turnAt).Milliseconds())
				r.update(hs, func(h *HostState) { h.Stage = "接続中" })
				release() // 次の1台にリンクの順番を譲る
			}
			if !connected && exists(base+".s2") {
				connected = true
				dbg.logf("%s 接続完了(.s2) 起動から%dms 合図から%dms", tag, time.Since(started).Milliseconds(), time.Since(turnAt).Milliseconds())
				r.update(hs, func(h *HostState) { h.Stage = "ログイン中" })
				n := unthrottleTree(cmd.Process.Pid)
				dbg.logf("%s 省電力による速度抑制を解除したプロセス数=%d", tag, n)
				release() // 「接続完了まで待つ」のときはここで次へ
			}
			if connected && !gotHost && exists(base+".s4") {
				gotHost = true
				name, logname := readHostFile(base + ".host")
				logPath = filepath.Join(p.LogDir, logname)
				dbg.logf("%s ログ記録開始(.s4) host=%s 起動から%dms", tag, name, time.Since(started).Milliseconds())
				r.update(hs, func(h *HostState) {
					h.Hostname = name
					if logname != "" {
						h.LogFile = filepath.Join(p.LogDir, logname)
					}
					h.Stage = "ログ取得中"
				})
			} else if connected && !gotHost && !sawLogin && exists(base+".s3") {
				sawLogin = true
				dbg.logf("%s ログイン通過(.s3) 起動から%dms", tag, time.Since(started).Milliseconds())
				r.update(hs, func(h *HostState) { h.Stage = "enable/前処理" })
			}
			if gotHost {
				n := lastCmd
				for n < len(t.Cmds) && exists(fmt.Sprintf("%s.c%d", base, n+1)) {
					n++
				}
				if n != lastCmd {
					lastCmd = n
					r.update(hs, func(h *HostState) { h.CmdDone = n })
				}
			}
			// 調査用: 10秒ごとにログファイルの伸び（取得速度）とTera Termの画面状態を記録する
			if dbg != nil && gotHost && time.Since(lastSpeed) >= 10*time.Second {
				if fi, err := os.Stat(logPath); err == nil {
					sec := time.Since(lastSpeed).Seconds()
					if !lastSpeed.IsZero() && lastSize >= 0 {
						dbg.logf("%s 速度 %d B/s (ログ %d KB) 画面=%s 実行中=%s", tag, int64(float64(fi.Size()-lastSize)/sec), fi.Size()/1024, ttWindowState(cmd.Process.Pid), t.Cmds[min(lastCmd, len(t.Cmds)-1)])
					}
					lastSize = fi.Size()
				}
				lastSpeed = time.Now()
			}
			// 時間は自分の番が来てから数える（順番待ちの間は数えない）
			if !gotTurn {
				break
			}
			el := time.Since(turnAt)
			if !connected && el > time.Duration(st.ConnectTimeout)*time.Second {
				kill("connect", "接続待ちタイムアウト")
				break loop
			}
			if st.HostTimeout > 0 && el > time.Duration(st.HostTimeout)*time.Second {
				kill("total", "上限時間超過")
				break loop
			}
		}
	}

	// 結果の読み取り
	name, logname := readHostFile(base + ".host")
	done := readLines(base + ".done")
	if dbg != nil {
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		var marks []string
		for _, f := range globMarkers(base) {
			if !strings.HasSuffix(f, ".ttl") {
				marks = append(marks, filepath.Ext(f))
			}
		}
		dbg.logf("%s マクロ終了 所要=%dms 終了コード=%d 打ち切り理由=%q マーカー=%v 結果ファイル=%q", tag, time.Since(started).Milliseconds(), code, killedWhy, marks, done)
	}
	r.update(hs, func(h *HostState) {
		h.Ended = nowMs()
		if name != "" {
			h.Hostname = name
		}
		if logname != "" {
			h.LogFile = filepath.Join(p.LogDir, logname)
		}
		switch killedWhy {
		case "abort":
			h.Status, h.Stage, h.Message = "aborted", "中断", "ユーザ操作で中断しました"
			return
		case "connect":
			h.Status, h.Stage = "error", "接続失敗"
			if handshake == "link" && !linked {
				h.Message = fmt.Sprintf("%d秒以内にTera Termとのリンクが終わりませんでした（Tera Termが起動直後に止まっています。続く場合は設定の「並列時の起動方法」を「接続完了まで待つ」にしてください）", st.ConnectTimeout) + diagNote
			} else {
				h.Message = fmt.Sprintf("%d秒以内に接続できませんでした（機器の応答なし、またはTera Termが確認ダイアログで止まっています）", st.ConnectTimeout) + diagNote
			}
			return
		case "total":
			h.Status, h.Stage = "error", "タイムアウト"
			h.Message = fmt.Sprintf("1台あたりの上限時間（%d秒）を超えたため打ち切りました", st.HostTimeout) + diagNote
			return
		}
		if len(done) == 0 {
			h.Status, h.Stage = "error", "異常終了"
			h.Message = "マクロが途中で終了しました（Tera Termを手で閉じた、またはマクロエラー）"
			return
		}
		get := func(i int) string {
			if i < len(done) {
				return strings.TrimSpace(done[i])
			}
			return ""
		}
		if get(2) != "" {
			h.Hostname = get(2)
		}
		if get(3) != "" {
			h.LogFile = filepath.Join(p.LogDir, get(3))
		}
		if get(0) == "OK" {
			h.CmdDone = h.CmdTotal
			nw, _ := strconv.Atoi(get(4))
			if nw > 0 {
				h.Status, h.Stage = "warn", "完了(要確認)"
				h.Message = fmt.Sprintf("%d個のコマンドで、出力が%d秒間止まったままプロンプトに戻りませんでした。ログの内容を確認してください", nw, t.CmdTimeout)
			} else {
				h.Status, h.Stage, h.Message = "ok", "完了", ""
			}
			return
		}
		h.Status, h.Stage = "error", "失敗"
		h.Message = errMessage(get(1))
		if get(1) == "CONNECT" || get(1) == "JUMP_CONNECT" {
			h.Message += "（Tera Termとのリンク、または機器への接続に失敗）"
		}
	})
	dbg.logf("%s 判定: %s / %s %s", tag, hs.Status, hs.Stage, hs.Message)
}

func (r *Runner) Abort() {
	r.mu.Lock()
	if r.job == nil || !r.job.Running || r.job.Aborting {
		r.mu.Unlock()
		return
	}
	r.job.Aborting = true
	ch := r.abortCh
	r.mu.Unlock()
	close(ch)
	r.onDirty()
}

// アプリ終了時の後始末
func (r *Runner) Shutdown() {
	r.Abort()
	deadline := time.Now().Add(5 * time.Second)
	for r.isRunning() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	os.RemoveAll(r.tmpRoot)
}

func tcpCheck(addr string, timeoutMs int) error {
	c, err := net.DialTimeout("tcp", addr, time.Duration(timeoutMs)*time.Millisecond)
	if err != nil {
		return err
	}
	c.Close()
	return nil
}

func globMarkers(base string) []string {
	m, _ := filepath.Glob(base + ".*")
	return m
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := strings.ReplaceAll(decodeText(b), "\r\n", "\n")
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func readHostFile(path string) (host, logname string) {
	ln := readLines(path)
	if len(ln) > 0 {
		host = strings.TrimSpace(ln[0])
	}
	if len(ln) > 1 {
		logname = strings.TrimSpace(ln[1])
	}
	return
}
