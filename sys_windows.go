//go:build windows

package main

// Windows固有の処理をここにまとめる（文字コード変換、プロセス管理、ファイル選択ダイアログなど）
// 外部ライブラリを使わず syscall だけで書いている

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	version  = syscall.NewLazyDLL("version.dll")

	pWideCharToMultiByte      = kernel32.NewProc("WideCharToMultiByte")
	pMultiByteToWideChar      = kernel32.NewProc("MultiByteToWideChar")
	pCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	pAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	pTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	pSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pMessageBoxW              = user32.NewProc("MessageBoxW")
	pShellExecuteW            = shell32.NewProc("ShellExecuteW")
	pSHBrowseForFolderW       = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromIDListW     = shell32.NewProc("SHGetPathFromIDListW")
	pGetOpenFileNameW         = comdlg32.NewProc("GetOpenFileNameW")
	pCoInitializeEx           = ole32.NewProc("CoInitializeEx")
	pCoUninitialize           = ole32.NewProc("CoUninitialize")
	pCoTaskMemFree            = ole32.NewProc("CoTaskMemFree")
	pGetFileVersionInfoSizeW  = version.NewProc("GetFileVersionInfoSizeW")
	pGetFileVersionInfoW      = version.NewProc("GetFileVersionInfoW")
	pVerQueryValueW           = version.NewProc("VerQueryValueW")
)

const cp932 = 932

func u16(s string) []uint16 { return append(utf16.Encode([]rune(s)), 0) }

func u16ptr(s string) *uint16 { return &u16(s)[0] }

// UTF-8 → Shift_JIS(CP932)。Tera Term 4系のマクロやCSV書き出しで使う
func toCP932(s string) []byte {
	if s == "" {
		return []byte{}
	}
	w := utf16.Encode([]rune(s))
	n, _, _ := pWideCharToMultiByte.Call(cp932, 0, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)), 0, 0, 0, 0)
	if n == 0 {
		return []byte(s)
	}
	buf := make([]byte, n)
	pWideCharToMultiByte.Call(cp932, 0, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)), uintptr(unsafe.Pointer(&buf[0])), n, 0, 0)
	return buf
}

func fromCP932(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	n, _, _ := pMultiByteToWideChar.Call(cp932, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return string(b)
	}
	buf := make([]uint16, n)
	pMultiByteToWideChar.Call(cp932, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&buf[0])), n)
	return string(utf16.Decode(buf))
}

// ---- プロセス管理 ----
// ttpmacro.exe とそこから起動される ttermpro.exe をジョブオブジェクトでひとまとめにして、
// 中断やタイムアウトのときに両方まとめて止められるようにする

type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobExtLimit struct {
	Basic                 jobBasicLimit
	IoInfo                [6]uint64
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type procHandle struct {
	cmd *exec.Cmd
	job uintptr
}

func attachProc(cmd *exec.Cmd) *procHandle {
	ph := &procHandle{cmd: cmd}
	h, _, _ := pCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return ph
	}
	var info jobExtLimit
	info.Basic.LimitFlags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	pSetInformationJobObject.Call(h, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	// PROCESS_SET_QUOTA | PROCESS_TERMINATE
	hp, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(cmd.Process.Pid))
	if err != nil {
		syscall.CloseHandle(syscall.Handle(h))
		return ph
	}
	r, _, _ := pAssignProcessToJobObject.Call(h, uintptr(hp))
	syscall.CloseHandle(hp)
	if r == 0 {
		syscall.CloseHandle(syscall.Handle(h))
		return ph
	}
	ph.job = h
	return ph
}

func (p *procHandle) kill() {
	if p.job != 0 {
		pTerminateJobObject.Call(p.job, 1)
		return
	}
	// ジョブに入れられなかった場合は taskkill で子プロセスごと止める
	c := exec.Command("taskkill", "/T", "/F", "/PID", itoa(p.cmd.Process.Pid))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if c.Run() != nil {
		p.cmd.Process.Kill()
	}
}

func (p *procHandle) close() {
	if p.job != 0 {
		syscall.CloseHandle(syscall.Handle(p.job))
		p.job = 0
	}
}

// ---- ダイアログ類 ----

type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        unsafe.Pointer
	dwReserved        uint32
	flagsEx           uint32
}

// ttpmacro.exe を選ぶ「ファイルを開く」ダイアログ。キャンセル時は空文字
func pickFile(title, initialDir string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// フィルタは \0 区切り・\0\0 終端
	var filter []uint16
	for _, s := range []string{"ttpmacro.exe", "ttpmacro.exe", "実行ファイル (*.exe)", "*.exe"} {
		filter = append(filter, utf16.Encode([]rune(s))...)
		filter = append(filter, 0)
	}
	filter = append(filter, 0)

	buf := make([]uint16, 1024)
	owner, _, _ := pGetForegroundWindow.Call()
	ofn := openFileName{
		hwndOwner:   owner,
		lpstrFilter: &filter[0],
		lpstrFile:   &buf[0],
		nMaxFile:    uint32(len(buf)),
		lpstrTitle:  u16ptr(title),
		flags:       0x00080000 | 0x00001000 | 0x00000800 | 0x00000008 | 0x00000004, // EXPLORER|FILEMUSTEXIST|PATHMUSTEXIST|NOCHANGEDIR|HIDEREADONLY
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	if initialDir != "" {
		if fi, err := os.Stat(initialDir); err == nil && fi.IsDir() {
			ofn.lpstrInitialDir = u16ptr(initialDir)
		}
	}
	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return "", nil
	}
	return syscall.UTF16ToString(buf), nil
}

type browseInfo struct {
	hwndOwner      uintptr
	pidlRoot       uintptr
	pszDisplayName *uint16
	lpszTitle      *uint16
	ulFlags        uint32
	lpfn           uintptr
	lParam         uintptr
	iImage         int32
}

// フォルダ選択ダイアログ。キャンセル時は空文字
func pickFolder(title string) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED
	defer pCoUninitialize.Call()

	disp := make([]uint16, 260)
	owner, _, _ := pGetForegroundWindow.Call()
	bi := browseInfo{
		hwndOwner:      owner,
		pszDisplayName: &disp[0],
		lpszTitle:      u16ptr(title),
		ulFlags:        0x0001 | 0x0010 | 0x0040, // RETURNONLYFSDIRS | EDITBOX | NEWDIALOGSTYLE
	}
	pidl, _, _ := pSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return "", nil
	}
	defer pCoTaskMemFree.Call(pidl)
	path := make([]uint16, 1024)
	r, _, _ := pSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
	if r == 0 {
		return "", errors.New("フォルダのパスを取得できませんでした")
	}
	return syscall.UTF16ToString(path), nil
}

// フォルダやファイルを既定のアプリで開く
func openPath(path string) error {
	r, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(u16ptr("open"))), uintptr(unsafe.Pointer(u16ptr(path))), 0, 0, 1)
	if r <= 32 {
		return errors.New("開けませんでした: " + path)
	}
	return nil
}

func fatalBox(msg string) {
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16ptr(msg))), uintptr(unsafe.Pointer(u16ptr(appName))), 0x10)
}

// ---- 画面（Edge/Chromeのアプリウィンドウ）----

func envDirs(keys ...string) []string {
	var out []string
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func findBrowser() string {
	roots := envDirs("ProgramFiles(x86)", "ProgramFiles", "ProgramW6432", "LocalAppData")
	for _, rel := range []string{`Microsoft\Edge\Application\msedge.exe`, `Google\Chrome\Application\chrome.exe`} {
		for _, r := range roots {
			p := filepath.Join(r, rel)
			if exists(p) {
				return p
			}
		}
	}
	return ""
}

// アプリ画面を開く。Edge(無ければChrome)のアプリモードで枠だけのウィンドウとして表示する
func openAppWindow(url string) error {
	if b := findBrowser(); b != "" {
		args := []string{"--app=" + url, "--window-size=1600,960", "--no-first-run", "--no-default-browser-check"}
		// 普段使いのブラウザと混ざらないよう専用のプロファイルで起動する
		if base := os.Getenv("LocalAppData"); base != "" {
			dir := filepath.Join(base, "NWLogTT", "ui")
			if os.MkdirAll(dir, 0o755) == nil {
				args = append(args, "--user-data-dir="+dir)
			}
		}
		if err := exec.Command(b, args...).Start(); err == nil {
			return nil
		}
	}
	// どちらも無ければ既定のブラウザで開く
	return openPath(url)
}

// ---- Tera Termの検出 ----

type ttCandidate struct {
	Path    string `json:"path"`
	Version string `json:"version"` // "4" / "5"
	Detail  string `json:"detail"`
}

// EXEのファイルバージョン(メジャー.マイナー)を読む。失敗したら 0,0
func fileVersion(path string) (int, int) {
	p := u16ptr(path)
	size, _, _ := pGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 {
		return 0, 0
	}
	buf := make([]byte, size)
	r, _, _ := pGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(p)), 0, size, uintptr(unsafe.Pointer(&buf[0])))
	if r == 0 {
		return 0, 0
	}
	var ptr unsafe.Pointer
	var l uint32
	r, _, _ = pVerQueryValueW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(u16ptr(`\`))), uintptr(unsafe.Pointer(&ptr)), uintptr(unsafe.Pointer(&l)))
	if r == 0 || l < 16 || ptr == nil {
		return 0, 0
	}
	// VS_FIXEDFILEINFO: signature, strucVersion, fileVersionMS, fileVersionLS ...
	ms := *(*uint32)(unsafe.Add(ptr, 8))
	return int(ms >> 16), int(ms & 0xffff)
}

func guessTTVersion(path string) (string, string) {
	maj, min := fileVersion(path)
	if maj >= 5 {
		return "5", "Tera Term " + itoa(maj) + "." + itoa(min)
	}
	if maj > 0 {
		return "4", "Tera Term " + itoa(maj) + "." + itoa(min)
	}
	if strings.Contains(strings.ToLower(filepath.Dir(path)), "teraterm5") {
		return "5", "Tera Term 5系（フォルダ名から推定）"
	}
	return "4", "Tera Term 4系（推定）"
}

func detectTeraTerm() []ttCandidate {
	var out []ttCandidate
	seen := map[string]bool{}
	roots := envDirs("ProgramFiles", "ProgramFiles(x86)", "ProgramW6432")
	if l := os.Getenv("LocalAppData"); l != "" {
		roots = append(roots, filepath.Join(l, "Programs"))
	}
	for _, r := range roots {
		for _, d := range []string{"teraterm5", "teraterm"} {
			p := filepath.Join(r, d, "ttpmacro.exe")
			k := strings.ToLower(p)
			if seen[k] || !exists(p) {
				continue
			}
			seen[k] = true
			v, det := guessTTVersion(p)
			out = append(out, ttCandidate{Path: p, Version: v, Detail: det})
		}
	}
	return out
}

func (p *procHandle) inJob() bool { return p.job != 0 }

// ---- 調査用：止まっているプロセスとウィンドウの様子を集める ----

var (
	pEnumWindows              = user32.NewProc("EnumWindows")
	pEnumChildWindows         = user32.NewProc("EnumChildWindows")
	pGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	pGetClassNameW            = user32.NewProc("GetClassNameW")
	pIsWindowVisible          = user32.NewProc("IsWindowVisible")
	pIsHungAppWindow          = user32.NewProc("IsHungAppWindow")
	pSendMessageTimeoutW      = user32.NewProc("SendMessageTimeoutW")

	enumMu  sync.Mutex
	enumOut []uintptr
	// コールバックは作り直すと枯渇するので1つを使い回す
	enumCB = syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		enumOut = append(enumOut, hwnd)
		return 1
	})
)

func enumTopWindows() []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumOut = nil
	pEnumWindows.Call(enumCB, 0)
	return append([]uintptr(nil), enumOut...)
}

func enumChildren(hwnd uintptr) []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumOut = nil
	pEnumChildWindows.Call(hwnd, enumCB, 0)
	return append([]uintptr(nil), enumOut...)
}

// 他プロセスのウィンドウ文字列。相手が固まっていても待ち続けないようタイムアウト付きで取る
func windowText(hwnd uintptr) string {
	buf := make([]uint16, 512)
	var res uintptr
	// WM_GETTEXT=0x000D, SMTO_ABORTIFHUNG=0x0002
	r, _, _ := pSendMessageTimeoutW.Call(hwnd, 0x000D, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), 0x0002, 300, uintptr(unsafe.Pointer(&res)))
	if r == 0 {
		return ""
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf))
}

func className(hwnd uintptr) string {
	buf := make([]uint16, 128)
	pGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

// rootPid(ttpmacro)とその子孫プロセス、それらが出しているウィンドウを一覧にする。
// 2つ目の戻り値は、止まる原因になっていそうなダイアログの文面
func diagSnapshot(rootPid int) (string, []string) {
	var b strings.Builder
	type pinfo struct {
		ppid uint32
		exe  string
	}
	procs := map[uint32]pinfo{}
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err == nil {
		var e syscall.ProcessEntry32
		e.Size = uint32(unsafe.Sizeof(e))
		for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
			procs[e.ProcessID] = pinfo{e.ParentProcessID, syscall.UTF16ToString(e.ExeFile[:])}
		}
		syscall.CloseHandle(snap)
	}
	// 子孫をたどる
	tree := map[uint32]bool{uint32(rootPid): true}
	for changed := true; changed; {
		changed = false
		for pid, pi := range procs {
			if !tree[pid] && tree[pi.ppid] {
				tree[pid] = true
				changed = true
			}
		}
	}
	for pid := range tree {
		name := procs[pid].exe
		if name == "" {
			name = "(終了済み)"
		}
		fmt.Fprintf(&b, "    プロセス pid=%d %s (親pid=%d)\r\n", pid, name, procs[pid].ppid)
	}

	var dialogs []string
	for _, h := range enumTopWindows() {
		var pid uint32
		pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		if !tree[pid] {
			continue
		}
		vis, _, _ := pIsWindowVisible.Call(h)
		hung, _, _ := pIsHungAppWindow.Call(h)
		cls, title := className(h), windowText(h)
		if vis == 0 && title == "" {
			continue // 内部用の見えないウィンドウは省く
		}
		fmt.Fprintf(&b, "    ウィンドウ pid=%d class=%s 表示=%v 応答なし=%v title=%q\r\n", pid, cls, vis != 0, hung != 0, title)
		if cls != "#32770" || vis == 0 {
			continue
		}
		// ダイアログなら中の文言も拾う（エラーメッセージや確認の内容がここに出る）
		var texts []string
		for _, c := range enumChildren(h) {
			if t := windowText(c); t != "" && len(texts) < 12 {
				texts = append(texts, strings.Join(strings.Fields(t), " "))
			}
		}
		fmt.Fprintf(&b, "      ダイアログ内の文言: %q\r\n", texts)
		// マクロ本体の小窓（MACRO - xxx.ttl）は原因ではないので画面には出さない
		if strings.HasPrefix(title, "MACRO - ") {
			continue
		}
		s := "「" + title + "」"
		if len(texts) > 0 {
			body := strings.Join(texts, " ")
			if r := []rune(body); len(r) > 160 {
				body = string(r[:160]) + "…"
			}
			s += " " + body
		}
		dialogs = append(dialogs, s)
	}
	return b.String(), dialogs
}

// ---- バックグラウンドで遅くならないようにする ----

var (
	pSetProcessInformation = kernel32.NewProc("SetProcessInformation")
	pIsIconic              = user32.NewProc("IsIconic")
)

// pidとその子孫のプロセスID
func treePids(rootPid int) []uint32 {
	parent := map[uint32]uint32{}
	snap, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return []uint32{uint32(rootPid)}
	}
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = syscall.Process32First(snap, &e); err == nil; err = syscall.Process32Next(snap, &e) {
		parent[e.ProcessID] = e.ParentProcessID
	}
	syscall.CloseHandle(snap)
	in := map[uint32]bool{uint32(rootPid): true}
	for changed := true; changed; {
		changed = false
		for pid, pp := range parent {
			if !in[pid] && in[pp] {
				in[pid] = true
				changed = true
			}
		}
	}
	out := make([]uint32, 0, len(in))
	for pid := range in {
		out = append(out, pid)
	}
	return out
}

type powerThrottling struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}

// Windowsは、前面にないウィンドウのプロセスを省電力のため遅く動かすことがある（特にWindows 11）。
// マクロとTera Termをその対象から外す。戻り値は設定できたプロセス数
func unthrottleTree(rootPid int) int {
	n := 0
	for _, pid := range treePids(rootPid) {
		// PROCESS_SET_INFORMATION
		h, err := syscall.OpenProcess(0x0200, false, pid)
		if err != nil {
			continue
		}
		// まず「実行速度の抑制」と「タイマー精度の抑制」の両方を外す。古いWindowsは後者を知らないので前者だけで再試行
		for _, mask := range []uint32{0x1 | 0x4, 0x1} {
			st := powerThrottling{Version: 1, ControlMask: mask, StateMask: 0}
			r, _, _ := pSetProcessInformation.Call(uintptr(h), 4, uintptr(unsafe.Pointer(&st)), unsafe.Sizeof(st))
			if r != 0 {
				n++
				break
			}
		}
		syscall.CloseHandle(h)
	}
	return n
}

// 調査用: Tera Termの画面がいまどういう状態か（前面/背面/最小化/非表示）
func ttWindowState(rootPid int) string {
	in := map[uint32]bool{}
	for _, p := range treePids(rootPid) {
		in[p] = true
	}
	fg, _, _ := pGetForegroundWindow.Call()
	for _, h := range enumTopWindows() {
		var pid uint32
		pGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&pid)))
		if !in[pid] || className(h) != "VTWin32" {
			continue
		}
		if vis, _, _ := pIsWindowVisible.Call(h); vis == 0 {
			return "非表示"
		}
		if ic, _, _ := pIsIconic.Call(h); ic != 0 {
			return "最小化"
		}
		if h == fg {
			return "前面(アクティブ)"
		}
		return "背面(非アクティブ)"
	}
	return "不明"
}

// ---- 更新まわり ----

// プロセスがまだ動いているか
func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(0x00100000, false, uint32(pid)) // SYNCHRONIZE
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	r, _ := syscall.WaitForSingleObject(h, 0)
	return r == 0x00000102 // WAIT_TIMEOUT = まだ終わっていない
}

func regString(k syscall.Handle, name string) (string, uint32, bool) {
	var typ, n uint32
	nm, _ := syscall.UTF16PtrFromString(name)
	if syscall.RegQueryValueEx(k, nm, nil, &typ, nil, &n) != nil || n == 0 {
		return "", 0, false
	}
	buf := make([]byte, n+2)
	if syscall.RegQueryValueEx(k, nm, nil, &typ, &buf[0], &n) != nil {
		return "", 0, false
	}
	if typ == syscall.REG_DWORD && n >= 4 {
		return "", uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24, true
	}
	u := make([]uint16, n/2)
	for i := range u {
		u[i] = uint16(buf[2*i]) | uint16(buf[2*i+1])<<8
	}
	return syscall.UTF16ToString(u), 0, true
}

// Windowsの「プロキシサーバーを使う」設定（手動設定のもの）を読む。
// 自動構成スクリプト(PAC)には対応していないので、その場合は画面側からの確認・ダウンロードに頼る
func systemProxy(r *http.Request) (*url.URL, error) {
	var k syscall.Handle
	path, _ := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Internet Settings`)
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, path, 0, syscall.KEY_READ, &k) != nil {
		return nil, nil
	}
	defer syscall.RegCloseKey(k)
	if _, on, ok := regString(k, "ProxyEnable"); !ok || on == 0 {
		return nil, nil
	}
	server, _, _ := regString(k, "ProxyServer")
	override, _, _ := regString(k, "ProxyOverride")
	host := strings.ToLower(r.URL.Hostname())
	for _, p := range strings.Split(override, ";") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || p == "<local>" {
			continue
		}
		if ok, _ := filepath.Match(p, host); ok {
			return nil, nil
		}
	}
	// "proxy:8080" または "http=proxy:8080;https=proxy:8443" の形
	pick := ""
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if i := strings.Index(part, "="); i >= 0 {
			if strings.EqualFold(part[:i], "https") {
				pick = part[i+1:]
			} else if pick == "" && strings.EqualFold(part[:i], "http") {
				pick = part[i+1:]
			}
		} else if pick == "" {
			pick = part
		}
	}
	if pick == "" {
		return nil, nil
	}
	if !strings.Contains(pick, "://") {
		pick = "http://" + pick
	}
	return url.Parse(pick)
}
