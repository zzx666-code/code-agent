package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveShellNonWSL(t *testing.T) {
	if runtime.GOOS != "windows" {
		exe, isPosix := resolveShell()
		if exe != "bash" || !isPosix {
			t.Fatalf("non-windows should return bash/posix, got %q/%v", exe, isPosix)
		}
		return
	}

	exe, isPosix := resolveShell()
	lower := strings.ToLower(exe)
	if strings.Contains(lower, `\system32\bash.exe`) || strings.Contains(lower, `\windowsapps\bash.exe`) {
		t.Fatalf("resolved shell is the WSL proxy: %s", exe)
	}
	if exe == "" {
		t.Fatal("resolved shell is empty")
	}
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("resolved shell does not exist: %s", exe)
	}
	t.Logf("resolved shell: %s posix=%v", exe, isPosix)
}

func TestBashToolRunsRealShell(t *testing.T) {
	tool := &BashTool{WorkDir: "."}
	res := tool.Execute(context.Background(), map[string]any{"command": "echo mewcode_shell_ok"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Output)
	}
	if !strings.Contains(res.Output, "mewcode_shell_ok") {
		t.Fatalf("expected echo output, got: %s", res.Output)
	}
}

func TestBashToolChainedCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cmd.exe fallback has no && between echo tokens guarantee; posix chain covered by Git Bash resolution test")
	}
	tool := &BashTool{WorkDir: "."}
	res := tool.Execute(context.Background(), map[string]any{"command": "true && echo chained_ok"})
	if res.IsError || !strings.Contains(res.Output, "chained_ok") {
		t.Fatalf("chained command failed: %s", res.Output)
	}
}

func TestBashToolWorkDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mewcode_marker.txt"), []byte("in_workdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &BashTool{WorkDir: dir}
	res := tool.Execute(context.Background(), map[string]any{"command": "cat mewcode_marker.txt"})
	if res.IsError {
		t.Fatalf("cat marker failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "in_workdir") {
		t.Fatalf("command did not run in workdir %s: %s", dir, res.Output)
	}
}
