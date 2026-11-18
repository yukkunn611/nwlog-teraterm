package main

// 機器種別ごとの既定値（プロンプト・特権モードへの入り方・前処理など）
// 詳細設定で書き換えられる。ホスト側で個別に上書きした項目はそちらが優先される

import "strings"

type DevType struct {
	Label          string `json:"label"`
	PromptUsername string `json:"promptUsername"` // ユーザ名の入力待ち
	PromptPassword string `json:"promptPassword"` // パスワードの入力待ち
	PromptUser     string `json:"promptUser"`     // ユーザモードの記号
	PromptEnable   string `json:"promptEnable"`   // 特権モードの記号
	EnableCmd      string `json:"enableCmd"`      // 特権モードに移行するコマンド。空なら移行しない
	PreCmds        string `json:"preCmds"`        // ログを取り始める前に送るコマンド（改行区切り。ログには残さない）
	ClockCmd       string `json:"clockCmd"`       // ログの先頭で時刻を確認するコマンド。空なら送らない
	Separator      string `json:"separator"`      // コマンド間の区切り。空なら送らない
	MoreStr        string `json:"moreStr"`        // ページ送りの表示。出てきたらスペースを送って先へ進める
}

var devTypeOrder = []string{"cisco", "fortigate", "yamaha", "other"}

func builtinDevTypes() map[string]DevType {
	return map[string]DevType{
		"cisco": {
			Label:          "Cisco IOS",
			PromptUsername: "sername:",
			PromptPassword: "assword:",
			PromptUser:     ">",
			PromptEnable:   "#",
			EnableCmd:      "enable",
			PreCmds:        "terminal length 0",
			ClockCmd:       "show clock",
			Separator:      "!",
			MoreStr:        "--More--",
		},
		"fortigate": {
			// FortiGateは特権モードの概念がなく、ログイン直後から # （読み取り専用は $）
			// ページ送りは設定を変えずに --More-- へスペースを返して進める
			Label:          "FortiGate",
			PromptUsername: "ogin:",
			PromptPassword: "assword:",
			PromptUser:     "$",
			PromptEnable:   "#",
			EnableCmd:      "",
			PreCmds:        "",
			ClockCmd:       "execute time",
			Separator:      "",
			MoreStr:        "--More--",
		},
		"yamaha": {
			// RTXは administrator で管理ユーザーに入る。行数制限は console lines infinity で外す（保存はしない）
			Label:          "YAMAHA RTX",
			PromptUsername: "sername:",
			PromptPassword: "assword:",
			PromptUser:     ">",
			PromptEnable:   "#",
			EnableCmd:      "administrator",
			PreCmds:        "console lines infinity",
			ClockCmd:       "show environment",
			Separator:      "",
			MoreStr:        "",
		},
		"other": {
			Label:          "汎用",
			PromptUsername: "sername:",
			PromptPassword: "assword:",
			PromptUser:     ">",
			PromptEnable:   "#",
			EnableCmd:      "",
			PreCmds:        "",
			ClockCmd:       "",
			Separator:      "",
			MoreStr:        "--More--",
		},
	}
}

func validDevType(k string) bool {
	for _, t := range devTypeOrder {
		if t == k {
			return true
		}
	}
	return false
}

// 足りない種別を補い、必須のプロンプトが空なら既定値に戻す
func normalizeDevTypes(m map[string]DevType) map[string]DevType {
	def := builtinDevTypes()
	out := map[string]DevType{}
	for _, k := range devTypeOrder {
		d, ok := m[k]
		if !ok {
			d = def[k]
		}
		d.Label = def[k].Label // 表示名は固定
		if strings.TrimSpace(d.PromptUsername) == "" {
			d.PromptUsername = def[k].PromptUsername
		}
		if strings.TrimSpace(d.PromptPassword) == "" {
			d.PromptPassword = def[k].PromptPassword
		}
		if strings.TrimSpace(d.PromptUser) == "" {
			d.PromptUser = def[k].PromptUser
		}
		if strings.TrimSpace(d.PromptEnable) == "" {
			d.PromptEnable = def[k].PromptEnable
		}
		d.EnableCmd = strings.TrimSpace(d.EnableCmd)
		d.ClockCmd = strings.TrimSpace(d.ClockCmd)
		d.Separator = strings.TrimSpace(d.Separator)
		d.MoreStr = strings.TrimSpace(d.MoreStr)
		d.PreCmds = strings.Join(splitCommands(d.PreCmds), "\n")
		out[k] = d
	}
	return out
}

// ホストの機器種別の既定値に、ホスト側の個別設定を重ねて実際に使う値を決める
func effectiveDevType(st Settings, h Host) DevType {
	k := h.DevType
	if !validDevType(k) {
		k = "cisco"
	}
	d := st.DevTypes[k]
	str := func(dst *string, v string) {
		if strings.TrimSpace(v) != "" {
			*dst = v
		}
	}
	// nil は「種別の既定値を使う」、空文字は「使わない」
	opt := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	str(&d.PromptUsername, h.PromptUsername)
	str(&d.PromptPassword, h.PromptPassword)
	str(&d.PromptUser, h.PromptUser)
	str(&d.PromptEnable, h.PromptEnable)
	opt(&d.EnableCmd, h.EnableCmd)
	opt(&d.ClockCmd, h.ClockCmd)
	opt(&d.Separator, h.Separator)
	if h.PreCmds != nil {
		d.PreCmds = strings.Join(splitCommands(*h.PreCmds), "\n")
	}
	return d
}
