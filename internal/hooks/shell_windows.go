package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// resolveHookShell 与 tools.resolveShell 逻辑一致（bash -> Git Bash -> cmd.exe），
// hooks 包独立持有副本以避免反向依赖 tools 包。
// 返回 (exePath, isPosix)。
type hookShellState struct {
	mu       sync.Mutex
	resolved bool
	exePath  string
	isPosix  bool
}

var hookShell hookShellState

func resolveHookShell() (string, bool) {
	if runtime.GOOS != "windows" {
		return "bash", true
	}
	s := &hookShell
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.resolved {
		s.resolve()
		s.resolved = true
	}
	return s.exePath, s.isPosix
}

func (s *hookShellState) accept(path string, isPosix bool) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	s.exePath = path
	s.isPosix = isPosix
	return true
}

func (s *hookShellState) isWSLProxy(path string) bool {
	lower := strings.ToLower(filepath.Clean(path))
	return strings.Contains(lower, `\system32\bash.exe`) || strings.Contains(lower, `\windowsapps\bash.exe`)
}

func (s *hookShellState) resolve() {
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
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if s.accept(path, true) {
			return
		}
	}
	s.accept(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), false)
}
