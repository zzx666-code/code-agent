package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// shellResolver 缓存解析结果，避免每次 Bash 调用都做 PATH 探测。
type shellResolver struct {
	mu        sync.Mutex
	resolved  bool
	exePath   string
	arg0      string // exe 本名，如 "bash.exe"、"cmd.exe"
	isPosix   bool  // true: sh 语义（-c, &&, $VAR）；false: cmd.exe 语义（/c）
}

var resolvedShell shellResolver

// resolveShell 返回 (exePath, isPosix)。
// Windows 上 System32\bash.exe 是 WSL legacy 代理：它会把命令交给 WSL 发行版执行。
// 若机器上只有 docker-desktop 之类的非标准发行版，/bin/bash 不存在，
// 代理启动即失败（execvpe(/bin/bash) failed），所有命令全部炸掉。
// 解析优先级：
//  1. MEWCODE_SHELL 环境变量（显式覆盖，指向 sh 语义 shell，如 Git Bash）
//  2. Git Bash 常见安装位置
//  3. PATH 上第一个非 WSL 代理的 bash
//  4. cmd.exe 兜底
// 非 Windows 平台恒定返回 ("bash", true)，行为与旧版一致。
func resolveShell() (string, bool) {
	if runtime.GOOS != "windows" {
		return "bash", true
	}
	s := &resolvedShell
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.resolved {
		s.resolveWindows()
		s.resolved = true
	}
	return s.exePath, s.isPosix
}

func (s *shellResolver) accept(path string, isPosix bool) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	s.exePath = path
	s.arg0 = strings.ToLower(filepath.Base(path))
	s.isPosix = isPosix
	return true
}

func (s *shellResolver) isWSLProxy(path string) bool {
	lower := strings.ToLower(filepath.Clean(path))
	return strings.Contains(lower, `\system32\bash.exe`) || strings.Contains(lower, `\windowsapps\bash.exe`)
}

func (s *shellResolver) resolveWindows() {
	if p := os.Getenv("MEWCODE_SHELL"); p != "" {
		if s.accept(p, true) {
			return
		}
	}

	for _, candidate := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Git", "bin", "bash.exe"),
		filepath.Join(os.Getenv("LocalAppData"), "Programs", "Git", "bin", "bash.exe"),
	} {
		if candidate == "" {
			continue
		}
		if s.accept(candidate, true) {
			return
		}
	}

	if path, err := exec.LookPath("bash"); err == nil && !s.isWSLProxy(path) {
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
		if s.accept(path, true) {
			return
		}
	}

	s.accept(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), false)
}
