package main

// 設定・プロファイル・コマンドセットの保存まわり
// 全部EXEと同じ場所の data フォルダにJSON/テキストで置く（インストール不要で持ち運べるように）

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ツール全体の設定（PCごと）
type Settings struct {
	TtpmacroPath string `json:"ttpmacroPath"`
	TTVersion    string `json:"ttVersion"`  // "4" or "5"
	WindowMode   string `json:"windowMode"` // normal / min / hidden

	LaunchIntervalMs int `json:"launchIntervalMs"`
	ConnectTimeout   int `json:"connectTimeout"` // TeraTermが接続完了するまでの待ち(秒)
	LoginTimeout     int `json:"loginTimeout"`
	CmdTimeout       int `json:"cmdTimeout"`
	HostTimeout      int `json:"hostTimeout"` // 1台あたりの上限(秒) 0で無制限
	CmdDelayMs       int `json:"cmdDelayMs"`

	SSHPort           int  `json:"sshPort"`
	TelnetPort        int  `json:"telnetPort"`
	NoSecurityWarning bool `json:"noSecurityWarning"`

	// v1.2.3までの項目。v1.3.0で機器種別(Cisco IOS)の既定値に引き継いだあとは使わない
	PromptUsername string  `json:"promptUsername,omitempty"`
	PromptPassword string  `json:"promptPassword,omitempty"`
	PromptUser     string  `json:"promptUser,omitempty"`
	PromptEnable   string  `json:"promptEnable,omitempty"`
	SendTermLen    *bool   `json:"sendTermLen,omitempty"`
	SendShowClock  *bool   `json:"sendShowClock,omitempty"`
	Separator      *string `json:"separator,omitempty"`

	// 機器種別ごとの既定値（cisco / fortigate / yamaha / other）
	DevTypes map[string]DevType `json:"devTypes"`

	Precheck          bool `json:"precheck"`
	PrecheckTimeoutMs int  `json:"precheckTimeoutMs"`

	OpenLogDirOnFinish bool `json:"openLogDirOnFinish"`
	NotifySound        bool `json:"notifySound"`

	JumpSSHCmd    string `json:"jumpSshCmd"`
	JumpTelnetCmd string `json:"jumpTelnetCmd"`
	JumpPrompts   string `json:"jumpPrompts"`

	LastProfile string `json:"lastProfile"`

	// 並列時、次の1台に順番を譲るタイミング。Tera Termを同時にリンクさせると固まるため1台ずつ通す
	//   "link"    … Tera Termとのリンクが済んだら次へ（速い。既定）
	//   "connect" … 機器への接続まで済んだら次へ（v1.3.1までの「1台ずつ接続完了を待つ」と同じ）
	StartupWait string `json:"startupWait"`
	// 調査用のログを data\debug に残す
	Debug bool `json:"debug"`
	// 設定ファイルの世代。既定値を変えたときの引き継ぎ判定に使う
	Schema int `json:"schema"`

	// 同意した利用規約の版。termsVersion と違えば同意画面を出す
	TermsAccepted string `json:"termsAccepted"`
	// 起動時に新しいバージョンを確認するか
	UpdateCheck bool `json:"updateCheck"`
	// 「このバージョンは再度表示しない」を選んだバージョン
	SkipVersion string `json:"skipVersion"`
}

func defaultSettings() Settings {
	return Settings{
		TTVersion:          "5",
		WindowMode:         "min",
		LaunchIntervalMs:   500,
		ConnectTimeout:     30,
		StartupWait:        "link",
		Schema:             6,
		UpdateCheck:        true,
		DevTypes:           builtinDevTypes(),
		LoginTimeout:       10,
		CmdTimeout:         60,
		HostTimeout:        3600,
		SSHPort:            22,
		TelnetPort:         23,
		NoSecurityWarning:  true,
		Precheck:           true,
		PrecheckTimeoutMs:  1500,
		OpenLogDirOnFinish: false,
		NotifySound:        true,
		JumpSSHCmd:         "ssh -l {user} {ip}",
		JumpTelnetCmd:      "telnet {ip}",
		JumpPrompts:        "> # $",
	}
}

type Host struct {
	ID     string `json:"id"`
	On     bool   `json:"on"`
	Group  string `json:"group"`
	Name   string `json:"name"`
	Proto  string `json:"proto"` // telnet / ssh
	IP     string `json:"ip"`
	Port   int    `json:"port"` // 0なら既定ポート
	CmdSet string `json:"cmdSet"`
	User   string `json:"user"`
	Pass   string `json:"pass"`
	Enable string `json:"enable"`
	// 個別上書き。0は全体設定を使う
	LoginTimeout int `json:"loginTimeout"`
	CmdTimeout   int `json:"cmdTimeout"`

	// 機器種別（cisco / fortigate / yamaha / other）
	DevType string `json:"devType"`
	// 機器種別の既定値を上書きする項目。空欄は種別の既定値を使う
	PromptUsername string `json:"promptUsername"`
	PromptPassword string `json:"promptPassword"`
	PromptUser     string `json:"promptUser"`
	PromptEnable   string `json:"promptEnable"`
	// こちらは null で種別の既定値、空文字で「使わない」
	EnableCmd *string `json:"enableCmd"`
	PreCmds   *string `json:"preCmds"`
	ClockCmd  *string `json:"clockCmd"`
	Separator *string `json:"separator"`
}

const maxConcurrency = 200

type Common struct {
	User   string `json:"user"`
	Pass   string `json:"pass"`
	Enable string `json:"enable"`
	CmdSet string `json:"cmdSet"`
}

type Jump struct {
	Enabled bool   `json:"enabled"`
	Proto   string `json:"proto"`
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Pass    string `json:"pass"`
}

// 案件ごとに切り替える単位
type Profile struct {
	Name        string `json:"name"`
	Ident       string `json:"ident"`
	LogDir      string `json:"logDir"`
	Mode        string `json:"mode"` // parallel / serial
	Concurrency int    `json:"concurrency"`
	Common      Common `json:"common"`
	Jump        Jump   `json:"jump"`
	Hosts       []Host `json:"hosts"`
}

type CmdSet struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Store struct {
	mu      sync.Mutex
	exeDir  string
	dataDir string
}

const defaultCommands = "terminal length 0\r\nshow run\r\nshow mac address-table\r\nshow log\r\nshow int status\r\nshow cdp nei\r\nshow cdp nei de\r\n"

func newStore() (*Store, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	exeDir := filepath.Dir(exe)
	if d := os.Getenv("NWLOG_HOME"); d != "" { // テスト用
		exeDir = d
	}
	s := &Store{exeDir: exeDir, dataDir: filepath.Join(exeDir, "data")}
	if err := s.prepare(); err != nil {
		// EXEの場所に書けない場合（Program Files等）はユーザ領域に逃がす
		base, e2 := os.UserConfigDir()
		if e2 != nil {
			return nil, err
		}
		s.dataDir = filepath.Join(base, "NWLogTT", "data")
		if e3 := s.prepare(); e3 != nil {
			return nil, e3
		}
	}
	return s, nil
}

func (s *Store) prepare() error {
	for _, d := range []string{s.dataDir, s.profDir(), s.cmdDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	// 書き込みテスト
	probe := filepath.Join(s.dataDir, ".probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err != nil {
		return err
	}
	os.Remove(probe)

	// 初回はコマンドセットとプロファイルを1つずつ用意しておく
	if names, _ := s.listCmdSets(); len(names) == 0 {
		os.WriteFile(filepath.Join(s.cmdDir(), "command.txt"), []byte(defaultCommands), 0o644)
	}
	if names := s.listProfiles(); len(names) == 0 {
		p := s.newProfile("既定")
		if err := s.saveProfile(p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) profDir() string       { return filepath.Join(s.dataDir, "profiles") }
func (s *Store) cmdDir() string        { return filepath.Join(s.dataDir, "commands") }
func (s *Store) settingsPath() string  { return filepath.Join(s.dataDir, "settings.json") }
func (s *Store) defaultLogDir() string { return filepath.Join(s.exeDir, "Log") }

func (s *Store) newProfile(name string) Profile {
	return Profile{
		Name:        name,
		LogDir:      s.defaultLogDir(),
		Mode:        "parallel",
		Concurrency: 10,
		Common:      Common{CmdSet: "command.txt"},
		Jump:        Jump{Proto: "ssh"},
		Hosts:       []Host{},
	}
}

// ファイル名に使えない文字が入っていないか
func validName(n string) bool {
	n = strings.TrimSpace(n)
	if n == "" || len(n) > 120 || n == "." || n == ".." {
		return false
	}
	return !strings.ContainsAny(n, `\/:*?"<>|`+"\r\n\t")
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) loadSettings() Settings {
	st := defaultSettings()
	b, err := os.ReadFile(s.settingsPath())
	if err == nil {
		st.Schema = 0
		json.Unmarshal(b, &st) // 無い項目は既定値のまま残る
		if st.Schema < 2 {
			// v1.0.0の設定を引き継ぐ。接続待ちが旧既定(20秒)のままなら新しい既定に上げる
			if st.ConnectTimeout == 20 {
				st.ConnectTimeout = 30
			}
			st.Schema = 2
		}
		if st.Schema < 3 {
			// 実機で show log が25秒かかる機器があったため、コマンド応答待ちが旧既定(30秒)のままなら60秒に上げる
			if st.CmdTimeout == 30 {
				st.CmdTimeout = 60
			}
			st.Schema = 3
		}
		if st.Schema < 4 {
			// show tech など数分かかるコマンドを流せるよう、1台あたりの上限が旧既定(15分)のままなら60分に上げる
			if st.HostTimeout == 900 {
				st.HostTimeout = 3600
			}
			st.Schema = 4
		}
		if st.Schema < 5 {
			// v1.3.0: 待ち受け文字列などを機器種別ごとの設定に移した。いままでの値はCisco IOSの既定値として引き継ぐ
			c := builtinDevTypes()["cisco"]
			if v, ok := st.DevTypes["cisco"]; ok {
				c = v
			}
			if st.PromptUsername != "" {
				c.PromptUsername = st.PromptUsername
			}
			if st.PromptPassword != "" {
				c.PromptPassword = st.PromptPassword
			}
			if st.PromptUser != "" {
				c.PromptUser = st.PromptUser
			}
			if st.PromptEnable != "" {
				c.PromptEnable = st.PromptEnable
			}
			if st.SendTermLen != nil && !*st.SendTermLen {
				c.PreCmds = ""
			}
			if st.SendShowClock != nil && !*st.SendShowClock {
				c.ClockCmd = ""
			}
			if st.Separator != nil {
				c.Separator = *st.Separator
			}
			if st.DevTypes == nil {
				st.DevTypes = map[string]DevType{}
			}
			st.DevTypes["cisco"] = c
			st.UpdateCheck = true
			st.Schema = 5
		}
		if st.Schema < 6 {
			// v1.3.2: 「1台ずつ接続完了を待つ」のON/OFFをやめ、リンクだけ1台ずつ行う方式を既定にした
			// （OFFだと同時リンクで固まり、ONだと遅かったため）
			st.StartupWait = "link"
			st.Schema = 6
		}
	}
	// 引き継いだら旧項目は捨てる
	st.PromptUsername, st.PromptPassword, st.PromptUser, st.PromptEnable = "", "", "", ""
	st.SendTermLen, st.SendShowClock, st.Separator = nil, nil, nil
	normalizeSettings(&st)
	return st
}

func normalizeSettings(st *Settings) {
	d := defaultSettings()
	if st.TTVersion != "4" && st.TTVersion != "5" {
		st.TTVersion = d.TTVersion
	}
	if st.WindowMode != "normal" && st.WindowMode != "min" && st.WindowMode != "hidden" {
		st.WindowMode = d.WindowMode
	}
	if st.StartupWait != "link" && st.StartupWait != "connect" {
		st.StartupWait = d.StartupWait
	}
	clamp := func(v *int, lo, hi, def int) {
		if *v < lo || *v > hi {
			*v = def
		}
	}
	clamp(&st.LaunchIntervalMs, 0, 10000, d.LaunchIntervalMs)
	clamp(&st.ConnectTimeout, 3, 600, d.ConnectTimeout)
	clamp(&st.LoginTimeout, 1, 600, d.LoginTimeout)
	clamp(&st.CmdTimeout, 1, 7200, d.CmdTimeout)
	clamp(&st.HostTimeout, 0, 86400, d.HostTimeout)
	clamp(&st.CmdDelayMs, 0, 60000, 0)
	clamp(&st.SSHPort, 1, 65535, 22)
	clamp(&st.TelnetPort, 1, 65535, 23)
	clamp(&st.PrecheckTimeoutMs, 200, 30000, d.PrecheckTimeoutMs)
	st.DevTypes = normalizeDevTypes(st.DevTypes)
	if strings.TrimSpace(st.JumpSSHCmd) == "" {
		st.JumpSSHCmd = d.JumpSSHCmd
	}
	if strings.TrimSpace(st.JumpTelnetCmd) == "" {
		st.JumpTelnetCmd = d.JumpTelnetCmd
	}
	if strings.TrimSpace(st.JumpPrompts) == "" {
		st.JumpPrompts = d.JumpPrompts
	}
}

func (s *Store) saveSettings(st Settings) error {
	normalizeSettings(&st)
	b, _ := json.MarshalIndent(st, "", "  ")
	return writeFileAtomic(s.settingsPath(), b)
}

func (s *Store) listProfiles() []string {
	ents, _ := os.ReadDir(s.profDir())
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			out = append(out, strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		}
	}
	sort.Strings(out)
	return out
}

func (s *Store) loadProfile(name string) (Profile, error) {
	if !validName(name) {
		return Profile{}, errors.New("プロファイル名が不正です")
	}
	b, err := os.ReadFile(filepath.Join(s.profDir(), name+".json"))
	if err != nil {
		return Profile{}, err
	}
	p := s.newProfile(name)
	if err := json.Unmarshal(b, &p); err != nil {
		return Profile{}, err
	}
	p.Name = name
	normalizeProfile(&p, s)
	return p, nil
}

func normalizeProfile(p *Profile, s *Store) {
	if p.Mode != "serial" {
		p.Mode = "parallel"
	}
	if p.Concurrency < 1 {
		p.Concurrency = 10
	}
	if p.Concurrency > maxConcurrency {
		p.Concurrency = maxConcurrency
	}
	if strings.TrimSpace(p.LogDir) == "" {
		p.LogDir = s.defaultLogDir()
	}
	if p.Jump.Proto != "telnet" {
		p.Jump.Proto = "ssh"
	}
	if p.Hosts == nil {
		p.Hosts = []Host{}
	}
	for i := range p.Hosts {
		if p.Hosts[i].Proto != "ssh" {
			p.Hosts[i].Proto = "telnet"
		}
		if !validDevType(p.Hosts[i].DevType) {
			p.Hosts[i].DevType = "cisco"
		}
	}
}

func (s *Store) saveProfile(p Profile) error {
	if !validName(p.Name) {
		return errors.New("プロファイル名に使えない文字が含まれています")
	}
	normalizeProfile(&p, s)
	b, _ := json.MarshalIndent(p, "", "  ")
	return writeFileAtomic(filepath.Join(s.profDir(), p.Name+".json"), b)
}

func (s *Store) deleteProfile(name string) error {
	if !validName(name) {
		return errors.New("プロファイル名が不正です")
	}
	return os.Remove(filepath.Join(s.profDir(), name+".json"))
}

func (s *Store) renameProfile(from, to string) error {
	if !validName(from) || !validName(to) {
		return errors.New("プロファイル名に使えない文字が含まれています")
	}
	dst := filepath.Join(s.profDir(), to+".json")
	if _, err := os.Stat(dst); err == nil {
		return errors.New("同じ名前のプロファイルが既にあります")
	}
	p, err := s.loadProfile(from)
	if err != nil {
		return err
	}
	p.Name = to
	if err := s.saveProfile(p); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.profDir(), from+".json"))
}

func (s *Store) listCmdSets() ([]CmdSet, error) {
	ents, err := os.ReadDir(s.cmdDir())
	if err != nil {
		return nil, err
	}
	var out []CmdSet
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.cmdDir(), e.Name()))
		if err != nil {
			continue
		}
		out = append(out, CmdSet{Name: e.Name(), Content: decodeText(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) loadCmdSet(name string) (string, error) {
	if !validName(name) {
		return "", errors.New("コマンドセット名が不正です")
	}
	b, err := os.ReadFile(filepath.Join(s.cmdDir(), name))
	if err != nil {
		return "", err
	}
	return decodeText(b), nil
}

func (s *Store) saveCmdSet(name, content string) error {
	if !validName(name) {
		return errors.New("コマンドセット名に使えない文字が含まれています")
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\n", "\r\n")
	return writeFileAtomic(filepath.Join(s.cmdDir(), name), []byte(content))
}

func (s *Store) deleteCmdSet(name string) error {
	if !validName(name) {
		return errors.New("コマンドセット名が不正です")
	}
	return os.Remove(filepath.Join(s.cmdDir(), name))
}

// コマンドセットの本文を1行ずつのコマンドにばらす（空行は送らない）
func splitCommands(content string) []string {
	var out []string
	for _, ln := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		ln = strings.TrimRight(ln, " \t\r")
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, ln)
	}
	return out
}
