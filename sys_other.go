//go:build !windows

package main

// Windows以外では動かす想定はないが、開発機(Linux)でロジックのテストを回すための代用品

import (
	"errors"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

func toCP932(s string) []byte   { return []byte(s) }
func fromCP932(b []byte) string { return string(b) }

type procHandle struct{ cmd *exec.Cmd }

func attachProc(cmd *exec.Cmd) *procHandle { return &procHandle{cmd: cmd} }
func (p *procHandle) kill()                { p.cmd.Process.Kill() }
func (p *procHandle) close()               {}
func (p *procHandle) inJob() bool          { return false }

func diagSnapshot(rootPid int) (string, []string) { return "", nil }
func unthrottleTree(rootPid int) int              { return 0 }
func ttWindowState(rootPid int) string            { return "不明" }

func pickFile(title, initialDir string) (string, error) {
	return "", errors.New("未対応のOSです")
}
func pickFolder(title string) (string, error) { return "", errors.New("未対応のOSです") }
func openPath(path string) error              { return nil }
func fatalBox(msg string)                     { println(msg) }
func openAppWindow(url string) error          { println("UI:", url); return nil }

type ttCandidate struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Detail  string `json:"detail"`
}

func detectTeraTerm() []ttCandidate { return nil }

func guessTTVersion(path string) (string, string) {
	if strings.Contains(strings.ToLower(filepath.Dir(path)), "teraterm5") {
		return "5", "Tera Term 5系（推定）"
	}
	return "4", "Tera Term 4系（推定）"
}

func processAlive(pid int) bool                     { return false }
func systemProxy(r *http.Request) (*url.URL, error) { return nil, nil }
