package main

// 1台ぶんのTTLマクロを組み立てる
// 流れはログイン→特権モード→前処理→logopen→時刻確認→区切りを挟んでコマンド。
// 並列で動かせるよう1ホスト1ファイルに分けている。
// プロンプトや特権モードへの入り方などは機器種別（DevType）の値を使う

import (
	"fmt"
	"regexp"
	"strings"
)

// 実行時に確定させた1台ぶんの情報（共通認証や既定ポートは解決済み）
type Target struct {
	Host         Host
	Port         int
	User         string
	Pass         string
	Enable       string
	LoginTimeout int
	CmdTimeout   int
	Cmds         []string
	DT           DevType // 機器種別の既定値に個別設定を重ねたもの
}

type TTLInput struct {
	St       Settings
	Jump     Jump // Enabled=falseなら直接接続
	Ident    string
	LogDir   string
	T        Target
	MarkBase string // 進捗マーカーファイルの接頭辞（拡張子なしのフルパス）
	Slot     int    // 並列時の何番目の枠か。ウィンドウ位置をずらすのに使う
	// デバッグモード時だけ指定する。マクロがどこまで進んだかを1行ずつ書き出すファイル
	TraceFile string
}

// TTLの文字列リテラルにする。' と " の両方が入っている場合は #39 でつなぐ
func ttlStr(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1 // 制御文字は捨てる
		}
		return r
	}, s)
	if s == "" {
		return "''"
	}
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	if !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	var b strings.Builder
	for i, p := range strings.Split(s, "'") {
		if i > 0 {
			b.WriteString("#39")
		}
		if p != "" {
			b.WriteString("'" + p + "'")
		}
	}
	return b.String()
}

// Tera Termのコマンドライン引数の値。空白や ; を含むなら "" で囲む（" は2つ重ねる）
func clArg(v string) string {
	if strings.ContainsAny(v, " ;\"\t") {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}

// connect に渡すコマンドライン
func connectLine(st Settings, proto, ip string, port int, user, pass string, slot int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s:%d", ip, port)
	if proto == "ssh" {
		fmt.Fprintf(&b, " /ssh /2 /auth=password /user=%s /passwd=%s", clArg(user), clArg(pass))
		if st.NoSecurityWarning {
			b.WriteString(" /nosecuritywarning")
		}
	} else {
		b.WriteString(" /nossh /T=1")
	}
	switch st.WindowMode {
	case "hidden":
		b.WriteString(" /V")
	case "min":
		b.WriteString(" /I")
	default:
		// 通常表示のときは重ならないよう少しずつずらして並べる
		x := 30 + (slot%6)*300
		y := 30 + ((slot/6)%4)*210
		fmt.Fprintf(&b, " /X=%d /Y=%d", x, y)
	}
	return b.String()
}

var hostClass = `([A-Za-z0-9_.:/@~\-]+)`

// プロンプトからホスト名を拾う正規表現。
//
//	Cisco      "SW01#"            → 1=SW01
//	FortiGate  "FGT60F # "        → 1=FGT60F 3=" "   （記号の前に空白が入る）
//	FortiGate  "FGT60F (root) # " → 1=FGT60F 2=" (root)" 3=" "（VDOM有効時）
//	FortiGate  "FGT-1(Primary) # " → 1=FGT-1 2="(Primary)" 3=" "（HA構成。括弧の前に空白が無い）
//	YAMAHA     "RTX1210> "        → 1=RTX1210
func promptRegex(sym string) string {
	// ※グループ2・3は空文字でも必ず一致する形にしておく。Tera Termは一致しなかったグループがあると
	//   範囲外を参照する作りのため（ttmdde.c の region->beg が -1 のまま使われる）
	return hostClass + `((?: ?\([^)\s]*\))*)( ?)` + regexp.QuoteMeta(sym)
}

func buildTTL(in TTLInput) string {
	st := in.St
	t := in.T
	relay := in.Jump.Enabled
	var b strings.Builder
	w := func(format string, a ...any) {
		if len(a) == 0 {
			b.WriteString(format)
		} else {
			fmt.Fprintf(&b, format, a...)
		}
		b.WriteString("\r\n")
	}
	debug := in.TraceFile != ""
	loginLogOpened := false
	// 失敗時に理由コードを持って終了処理へ飛ぶ。そのときのresultも控えておく（調査用）
	failIf := func(cond, code string) {
		w("if %s then", cond)
		w("\tdres = result")
		w("\terrcode = '%s'", code)
		w("\tgoto finish")
		w("endif")
	}
	// デバッグモードのときだけ、通過点をトレースファイルに残す
	// ※ trace を呼ぶと result が書き換わるので、result を見終わった後にしか置かないこと
	trace := func(msg string) {
		if debug {
			w("tmsg = %s", ttlStr(msg))
			w("call trace")
		}
	}
	// connect。Tera Termとのリンクに失敗した(result=0)ときは少し待ってやり直す
	// 複数台を同時に立ち上げた直後はリンクを取りこぼすことがあるため
	connect := func(line, failCode string) {
		w("ctry = 0")
		w(":doconnect")
		trace("connect 発行")
		w("connect %s", ttlStr(line))
		w("cres = result")
		if debug {
			w("int2str scres cres")
			w("tmsg = 'connect 戻り result='")
			w("strconcat tmsg scres")
			w("strconcat tmsg ' (0=リンク失敗 1=リンクのみ 2=接続完了)'")
			w("call trace")
		}
		w("if cres = 0 then")
		w("\tctry = ctry + 1")
		w("\tif ctry < 3 then")
		w("\t\tmpause 2000")
		w("\t\tgoto doconnect")
		w("\tendif")
		w("endif")
		w("if cres <> 2 then")
		w("\tdres = cres")
		w("\terrcode = '%s'", failCode)
		w("\tgoto finish")
		w("endif")
		if debug && !loginLogOpened {
			// 調査用: ログインからプロンプト判定までのやり取りをそのまま残す（本番のログを開く前に閉じる）
			loginLogOpened = true
			w("logopen %s 0 0 0 0 1", ttlStr(strings.TrimSuffix(in.TraceFile, "_trace.txt")+"_login.txt"))
			w("logging = 2")
		}
	}
	mark := func(name string) {
		w("mkname = '%s'", name)
		w("call mark")
	}

	dt := t.DT
	pU := ttlStr(dt.PromptUsername)
	pP := ttlStr(dt.PromptPassword)
	pUser := ttlStr(dt.PromptUser)
	pEn := ttlStr(dt.PromptEnable)
	reEn := ttlStr(promptRegex(dt.PromptEnable))
	reUser := ttlStr(promptRegex(dt.PromptUser))
	// 踏み台へのログインは機器種別に関係なく、よくある表記をまとめて待つ
	jpU := "'sername:' 'ogin:'"
	jpP := "'assword:'"

	route := "直接接続"
	if relay {
		route = "踏み台経由"
	}
	w("; ================================================================")
	w("; %s が自動生成したマクロです", appName)
	w("; 対象: %s  %s (%s / %s / %s)", sanitizeComment(t.Host.Name), t.Host.IP, t.Host.Proto, dt.Label, route)
	w("; 実行が終わると自動で削除されます")
	w("; ================================================================")
	w("mkbase = %s", ttlStr(in.MarkBase))
	w("logdir = %s", ttlStr(strings.TrimRight(in.LogDir, `\/`)))
	w("ident = %s", ttlStr(in.Ident))
	w("targetip = %s", ttlStr(t.Host.IP))
	w("rstat = 'ERR'")
	w("errcode = ''")
	w("hostname = ''")
	w("jhost = ''")
	w("logname = ''")
	w("nwarn = 0")
	w("logging = 0")
	w("privileged = 0")
	w("dres = -1")
	w("quietchk = 0")
	w("vdom = ''")
	w("spc = ''")
	w("idlesec = %d", t.CmdTimeout)
	if debug {
		w("tracefile = %s", ttlStr(in.TraceFile))
	}
	w("")
	trace("マクロ開始")
	mark(".s1")
	w("")

	// ---------------- 接続 ----------------
	if !relay {
		w("; --- 対象機器へ直接つなぐ ---")
		connect(connectLine(st, t.Host.Proto, t.Host.IP, t.Port, t.User, t.Pass, in.Slot), "CONNECT")
		mark(".s2")
		w("timeout = %d", t.LoginTimeout)
		if t.Host.Proto == "ssh" {
			w("; SSHは接続時に認証まで済んでいるのでプロンプトだけ待つ")
			w("wait %s %s", pUser, pEn)
			failIf("result = 0", "LOGIN_TIMEOUT")
		} else {
			writeTelnetLogin(w, failIf, pU, pP, pUser, pEn, t.User, t.Pass, "LOGIN_PROMPT", "LOGIN_TIMEOUT", "AUTH")
		}
	} else {
		j := in.Jump
		jport := j.Port
		if jport == 0 {
			if j.Proto == "ssh" {
				jport = st.SSHPort
			} else {
				jport = st.TelnetPort
			}
		}
		jp := strings.Fields(st.JumpPrompts)
		if len(jp) > 7 {
			jp = jp[:7]
		}
		var jpl []string
		for _, p := range jp {
			jpl = append(jpl, ttlStr(p))
		}
		jpAll := strings.Join(jpl, " ")

		w("; --- まず踏み台へログインする ---")
		connect(connectLine(st, j.Proto, j.IP, jport, j.User, j.Pass, in.Slot), "JUMP_CONNECT")
		mark(".s2")
		w("timeout = %d", t.LoginTimeout+5)
		if j.Proto == "ssh" {
			w("wait %s", jpAll)
			failIf("result = 0", "JUMP_LOGIN")
		} else {
			w("wait %s %s", jpU, jpP)
			failIf("result = 0", "JUMP_LOGIN")
			w("if result < 3 then")
			w("\tsendln %s", ttlStr(j.User))
			w("\twait %s", jpP)
			w("endif")
			failIf("result = 0", "JUMP_LOGIN")
			w("sendln %s", ttlStr(j.Pass))
			w("wait %s %s %s", jpAll, jpP, jpU)
			failIf("result = 0", "JUMP_LOGIN")
			failIf(fmt.Sprintf("result > %d", len(jpl)), "JUMP_AUTH")
		}
		w("; 踏み台自身のホスト名を控えておく（対象に入れず踏み台のログを取ってしまう事故の検知用）")
		w("mpause 500")
		w("flushrecv")
		w("sendln ''")
		w("timeout = 3")
		w("waitregex %s %s", reEn, reUser)
		w("if result > 0 then")
		w("\tjhost = groupmatchstr1")
		w("endif")
		w("mpause 300")
		w("flushrecv")
		trace("踏み台ログイン完了")
		w("")
		w("; --- 踏み台から対象機器へ ---")
		w("timeout = %d", t.LoginTimeout+10)
		if t.Host.Proto == "ssh" {
			cmd := relayCmd(st.JumpSSHCmd, t)
			w("sendln %s", ttlStr(cmd))
			w("wait %s '(yes/no'", pP)
			w("if result = 2 then")
			w("\tsendln 'yes'")
			w("\twait %s", pP)
			w("endif")
			failIf("result = 0", "CONNECT")
			w("sendln %s", ttlStr(t.Pass))
			w("wait %s %s %s", pUser, pEn, pP)
			failIf("result = 0", "LOGIN_TIMEOUT")
			failIf("result = 3", "AUTH")
		} else {
			cmd := relayCmd(st.JumpTelnetCmd, t)
			w("sendln %s", ttlStr(cmd))
			writeTelnetLogin(w, failIf, pU, pP, pUser, pEn, t.User, t.Pass, "CONNECT", "LOGIN_TIMEOUT", "AUTH")
		}
	}
	mark(".s3")
	trace("ログイン通過（プロンプト記号を受信）")
	w("")

	// ---------------- プロンプト確定 ----------------
	w("; --- プロンプトからホスト名を拾う ---")
	w("; バナーの残りを読み飛ばしてから空Enterを送り、返ってきた行で判定する")
	w("timeout = %d", t.LoginTimeout)
	w("mpause 500")
	w("flushrecv")
	w("sendln ''")
	w("waitregex %s %s", reEn, reUser)
	w("hres = result")
	w("if hres = 0 then")
	w("\t; 1回目で拾えなければ、もう一度Enterを送って試す（ログイン直後の表示が長い機器向け）")
	w("\tsendln ''")
	w("\ttimeout = 5")
	w("\twaitregex %s %s", reEn, reUser)
	w("\thres = result")
	w("endif")
	w("if hres > 0 then")
	w("\thostname = groupmatchstr1")
	w("\tvdom = groupmatchstr2")
	w("\tspc = groupmatchstr3")
	w("endif")
	w("if hres = 1 then")
	w("\tprivileged = 1")
	w("endif")
	if debug {
		w("tmsg = 'ホスト名判定 hostname='")
		w("strconcat tmsg hostname")
		w("call trace")
	}
	if relay {
		w("; 踏み台と同じホスト名のままなら対象に入れていない")
		w("strlen jhost")
		w("if result > 0 then")
		w("\tstrcompare hostname jhost")
		w("\tif result = 0 then")
		w("\t\terrcode = 'RELAY'")
		w("\t\tgoto finish")
		w("\tendif")
		w("endif")
	}
	w("")

	// ---------------- 特権モード ----------------
	w("pch = %s", pEn)
	if dt.EnableCmd == "" || t.Enable == "" {
		if dt.EnableCmd == "" {
			w("; 特権モードに移行するコマンドが設定されていないので移行しない")
		} else {
			w("; 特権モード用のパスワードが空欄なので移行しない（最初から特権ならそのまま）")
		}
		w("if privileged = 0 then")
		w("\tpch = %s", pUser)
		w("endif")
	} else {
		w("; --- 特権モードへ（ログイン直後から特権モードなら飛ばす） ---")
		w("if privileged = 0 then")
		w("\tsendln %s", ttlStr(dt.EnableCmd))
		w("\twait %s %s", pP, pEn)
		w("\tif result = 0 then")
		w("\t\terrcode = 'ENABLE_TIMEOUT'")
		w("\t\tgoto finish")
		w("\tendif")
		w("\tif result = 1 then")
		w("\t\tsendln %s", ttlStr(t.Enable))
		w("\t\twait %s %s '%% ' 'rror'", pEn, pP)
		w("\t\tif result <> 1 then")
		w("\t\t\terrcode = 'ENABLE_AUTH'")
		w("\t\t\tgoto finish")
		w("\t\tendif")
		w("\tendif")
		w("endif")
	}
	w("")
	trace("enable 通過")
	w("; 以降の待ち受けは「ホスト名＋プロンプト記号」で行う（FortiGateの空白・VDOM表記もそのまま含める）")
	w("strlen hostname")
	w("if result = 0 then")
	w("\t; ホスト名が拾えなかった場合は、特権・ユーザどちらの記号でも終わりとみなす。")
	w("\t; （どちらのモードか分からないので片方に決め打ちしない。出力中の記号を誤検知しないよう静かになったかも確かめる）")
	w("\tpr1 = %s", pEn)
	w("\tpr2 = %s", pUser)
	w("\thostname = targetip")
	w("\tquietchk = 1")
	w("else")
	w("\tpr1 = hostname")
	w("\tstrconcat pr1 vdom")
	w("\tstrconcat pr1 spc")
	w("\tstrconcat pr1 pch")
	w("\t; conf t などで (config)# になっても止まらないようにしておく")
	w("\tpr2 = ')'")
	w("\tstrconcat pr2 spc")
	w("\tstrconcat pr2 pch")
	w("endif")
	if debug {
		w("tmsg = '待ち受けるプロンプト='")
		w("strconcat tmsg pr1")
		w("call trace")
	}
	w("")

	if pre := splitCommands(dt.PreCmds); len(pre) > 0 {
		w("; 前処理（ログには残さない）")
		for _, c := range pre {
			w("sendln %s", ttlStr(c))
			w("call waitcmd")
		}
		w("")
	}

	// ---------------- ログ ----------------
	w("; --- ログファイル名  識別子_ホスト名_YYYYMMDD-HHMMSS.txt ---")
	w("getdate datestr")
	w("strcopy datestr 1 4 yearstr")
	w("strcopy datestr 6 2 monthstr")
	w("strcopy datestr 9 2 daystr")
	w("gettime timestr")
	w("strcopy timestr 1 2 hourstr")
	w("strcopy timestr 4 2 minutestr")
	w("strcopy timestr 7 2 secondstr")
	w("logname = ''")
	w("strlen ident")
	w("if result > 0 then")
	w("\tlogname = ident")
	w("\tstrconcat logname '_'")
	w("endif")
	w("strconcat logname hostname")
	w("strconcat logname '_'")
	w("strconcat logname yearstr")
	w("strconcat logname monthstr")
	w("strconcat logname daystr")
	w("strconcat logname '-'")
	w("strconcat logname hourstr")
	w("strconcat logname minutestr")
	w("strconcat logname secondstr")
	w("strconcat logname '.txt'")
	w("logpath = logdir")
	w("strconcat logpath '\\'")
	w("strconcat logpath logname")
	w("")
	w("; ホスト名をアプリ側へ知らせる")
	w("mkfile = mkbase")
	w("strconcat mkfile '.host'")
	w("filecreate mfh mkfile")
	w("if result = 0 then")
	w("\tfilewriteln mfh hostname")
	w("\tfilewriteln mfh logname")
	w("\tfileclose mfh")
	w("endif")
	w("")
	if debug {
		w("if logging = 2 then")
		w("\tlogclose")
		w("\tlogging = 0")
		w("endif")
	}
	w("logopen logpath 0 1 0 0 1")
	failIf("result <> 0", "LOGOPEN")
	w("logging = 1")
	mark(".s4")
	trace("ログ記録開始")
	w("")
	w("; コマンドの待ち方: 出力が流れ続けている間は何分でも待ち、%d秒間なにも出てこなくなったら無応答とみなす", t.CmdTimeout)
	w("sendln ''")
	w("call waitcmd")
	if dt.ClockCmd != "" {
		w("sendln %s", ttlStr(dt.ClockCmd))
		w("call waitcmd")
	}
	w("")
	w("; --- コマンド取得 ---")
	for i, c := range t.Cmds {
		if dt.Separator != "" {
			w("sendln %s", ttlStr(dt.Separator))
			w("call waitcmd")
		}
		w("sendln %s", ttlStr(c))
		w("call waitcmd")
		w("if wres = 0 then")
		w("\tnwarn = nwarn + 1")
		if debug {
			w("\ttmsg = %s", ttlStr(fmt.Sprintf("コマンド %d/%d 無応答で打ち切り: %s", i+1, len(t.Cmds), c)))
			w("\tcall trace")
		}
		w("endif")
		mark(fmt.Sprintf(".c%d", i+1))
		trace(fmt.Sprintf("コマンド %d/%d 完了: %s", i+1, len(t.Cmds), c))
		if st.CmdDelayMs > 0 {
			w("mpause %d", st.CmdDelayMs)
		}
	}
	w("")
	w("rstat = 'OK'")
	w("")

	// ---------------- 終了処理 ----------------
	w(":finish")
	w("if logging > 0 then")
	w("\tlogclose")
	w("endif")
	w("int2str swarn nwarn")
	if debug {
		w("int2str sdres dres")
		w("tmsg = '終了処理 rstat='")
		w("strconcat tmsg rstat")
		w("strconcat tmsg ' errcode='")
		w("strconcat tmsg errcode")
		w("strconcat tmsg ' 失敗時のresult='")
		w("strconcat tmsg sdres")
		w("strconcat tmsg ' 応答なしコマンド数='")
		w("strconcat tmsg swarn")
		w("call trace")
	}
	w("mkfile = mkbase")
	w("strconcat mkfile '.done'")
	w("filecreate mfh mkfile")
	w("if result = 0 then")
	w("\tfilewriteln mfh rstat")
	w("\tfilewriteln mfh errcode")
	w("\tfilewriteln mfh hostname")
	w("\tfilewriteln mfh logname")
	w("\tfilewriteln mfh swarn")
	w("\tfileclose mfh")
	w("endif")
	w("; Tera Termとつながっている時だけ閉じる（未リンクでclosettするとエラーダイアログで止まるため）")
	w("testlink")
	w("lk = result")
	if relay {
		w("if lk = 2 then")
		w("\tsendln 'exit'")
		w("\tmpause 300")
		w("endif")
	}
	w("if lk > 0 then")
	w("\tclosett")
	w("endif")
	w("end")
	w("")
	w("; 進捗マーカー（空ファイル）を作るだけのサブルーチン")
	w(":mark")
	w("mkfile = mkbase")
	w("strconcat mkfile mkname")
	w("filecreate mfh mkfile")
	w("if result = 0 then")
	w("\tfileclose mfh")
	w("endif")
	w("return")
	w("")
	w("; コマンドの終わり(プロンプト)を待つサブルーチン。結果は wres (1=戻った 0=無応答)")
	w("; show tech のように何分もかかるコマンドでも、出力が続いている限り打ち切らない")
	w(":waitcmd")
	more := ""
	if dt.MoreStr != "" {
		more = " " + ttlStr(dt.MoreStr)
	}
	w("wres = 1")
	w(":wc_loop")
	w("; まず5秒だけ普通にプロンプトを待つ（ほとんどのコマンドはここで終わる）")
	w("timeout = 5")
	w("wait pr1 pr2%s", more)
	if more != "" {
		w("if result = 3 goto wc_more")
	}
	w("if result > 0 goto wc_got")
	w("; まだ終わっていない。プロンプトか、次の1行(改行)が来るかを見る。改行が来たら出力中なので待ち直す")
	w("timeout = idlesec")
	w("wait pr1 pr2 #10%s", more)
	if more != "" {
		w("if result = 4 goto wc_more")
	}
	w("if result = 3 goto wc_loop")
	w("if result > 0 goto wc_got")
	w("; idlesec秒なにも出てこなかった。空Enterを送ってプロンプトに戻っているかだけ確かめる")
	w("sendln ''")
	w("timeout = 5")
	w("wait pr1 pr2")
	w("if result > 0 goto wc_got")
	w("wres = 0")
	w("goto wc_end")
	w(":wc_more")
	w("; ページ送りの表示が出たらスペースで先へ進める（機器の設定は変えない）")
	w("send ' '")
	w("goto wc_loop")
	w(":wc_got")
	w("if quietchk = 0 goto wc_end")
	w("; ホスト名が分からない機器だけ: 記号のあと1秒以内に次の行が来たら、出力の途中の記号だったとみなして待ち直す")
	w("timeout = 0")
	w("mtimeout = 1000")
	w("wait #10%s", more)
	w("qres = result")
	w("mtimeout = 0")
	w("if qres = 1 goto wc_loop")
	if more != "" {
		w("if qres = 2 goto wc_more")
	}
	w(":wc_end")
	w("mtimeout = 0")
	w("return")
	if debug {
		w("")
		w("; 調査用: 時刻付きで1行追記する")
		w(":trace")
		w("gettime ttime")
		w("tline = ttime")
		w("strconcat tline ' '")
		w("strconcat tline tmsg")
		w("fileopen tfh tracefile 1")
		w("if tfh <> -1 then")
		w("\tfilewriteln tfh tline")
		w("\tfileclose tfh")
		w("endif")
		w("return")
	}
	return b.String()
}

// Telnetのログイン。Username:が出る機器とPassword:だけの機器(ライン認証)の両方を1回のwaitでさばく
func writeTelnetLogin(w func(string, ...any), failIf func(string, string), pU, pP, pUser, pEn, user, pass, codeNoPrompt, codeTimeout, codeAuth string) {
	w("wait %s %s", pU, pP)
	failIf("result = 0", codeNoPrompt)
	w("if result = 1 then")
	w("\tsendln %s", ttlStr(user))
	w("\twait %s", pP)
	w("endif")
	failIf("result = 0", codeNoPrompt)
	w("sendln %s", ttlStr(pass))
	w("; もう一度 Password: / Username: が出たら認証失敗")
	w("wait %s %s %s %s", pUser, pEn, pP, pU)
	failIf("result = 0", codeTimeout)
	failIf("result > 2", codeAuth)
}

func relayCmd(tpl string, t Target) string {
	r := strings.NewReplacer("{user}", t.User, "{ip}", t.Host.IP, "{port}", fmt.Sprint(t.Port))
	return r.Replace(tpl)
}

func sanitizeComment(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, s)
}

// 理由コード → 画面に出す文言
func errMessage(code string) string {
	switch code {
	case "CONNECT":
		return "接続できませんでした"
	case "LOGIN_PROMPT":
		return "ログインプロンプトが出ませんでした"
	case "LOGIN_TIMEOUT":
		return "ログイン後のプロンプトが確認できませんでした（認証失敗の可能性）"
	case "AUTH":
		return "認証に失敗しました（ユーザ名/パスワード）"
	case "ENABLE_TIMEOUT":
		return "enableの応答がありませんでした"
	case "ENABLE_AUTH":
		return "enableパスワードが違います"
	case "LOGOPEN":
		return "ログファイルを作成できませんでした（保存先を確認）"
	case "JUMP_CONNECT":
		return "踏み台に接続できませんでした"
	case "JUMP_LOGIN":
		return "踏み台にログインできませんでした"
	case "JUMP_AUTH":
		return "踏み台の認証に失敗しました"
	case "RELAY":
		return "踏み台から対象機器に入れませんでした"
	case "":
		return "原因不明のエラー"
	}
	return "エラー: " + code
}
