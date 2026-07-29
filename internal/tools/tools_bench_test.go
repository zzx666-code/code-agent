package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- 工具调用单元测试 ---

// TestReadFileTool: 读取文件内容带行号
func TestReadFileTool(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)
	tool := &ReadFileTool{FileStateCache: NewFileStateCache()}
	res := tool.Execute(context.Background(), map[string]any{"file_path": path})
	if res.IsError {
		t.Errorf("ReadFile failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "line1") || !strings.Contains(res.Output, "line2") {
		t.Errorf("ReadFile output missing content: %s", res.Output)
	}
}

// TestReadFileMissing: 读不存在的文件返回错误
func TestReadFileMissing(t *testing.T) {
	tool := &ReadFileTool{FileStateCache: NewFileStateCache()}
	res := tool.Execute(context.Background(), map[string]any{"file_path": "/nonexistent/file.txt"})
	if !res.IsError {
		t.Error("expected error for missing file")
	}
}

// TestWriteFileTool: 写入文件并创建父目录
func TestWriteFileTool(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "dir", "file.txt")
	tool := &WriteFileTool{FileStateCache: NewFileStateCache()}
	res := tool.Execute(context.Background(), map[string]any{"file_path": path, "content": "hello"})
	if res.IsError {
		t.Errorf("WriteFile failed: %s", res.Output)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello" {
		t.Errorf("file content = %q, want 'hello'", string(data))
	}
}

// TestEditFileTool: 替换文件内容
func TestEditFileTool(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	os.WriteFile(path, []byte("package main\n\nfunc old() {}\n"), 0o644)
	fsc := NewFileStateCache()
	info, _ := os.Stat(path)
	fsc.Record(path, "package main\n\nfunc old() {}\n", info.ModTime().UnixMilli())
	tool := &EditFileTool{FileStateCache: fsc}
	res := tool.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "func old() {}",
		"new_string": "func new() {}",
	})
	if res.IsError {
		t.Errorf("EditFile failed: %s", res.Output)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "func new() {}") {
		t.Errorf("edit didn't apply: %s", string(data))
	}
	if strings.Contains(string(data), "func old() {}") {
		t.Error("old string still present after edit")
	}
}

// TestEditFileNotUnique: old_string 不唯一时报错
func TestEditFileNotUnique(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "dup.txt")
	os.WriteFile(path, []byte("dup\ndup\n"), 0o644)
	fsc := NewFileStateCache()
	info, _ := os.Stat(path)
	fsc.Record(path, "dup\ndup\n", info.ModTime().UnixMilli())
	tool := &EditFileTool{FileStateCache: fsc}
	res := tool.Execute(context.Background(), map[string]any{
		"file_path":  path,
		"old_string": "dup",
		"new_string": "unique",
	})
	if !res.IsError {
		t.Error("expected error for non-unique old_string")
	}
}

// TestGlobTool: glob 匹配文件
func TestGlobTool(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "src"), 0o755)
	os.WriteFile(filepath.Join(tmp, "src", "a.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(tmp, "src", "b.go"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(tmp, "src", "c.txt"), []byte("x"), 0o644)
	tool := &GlobTool{}
	res := tool.Execute(context.Background(), map[string]any{"pattern": "**/*.go", "path": tmp})
	if res.IsError {
		t.Errorf("Glob failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "a.go") || !strings.Contains(res.Output, "b.go") {
		t.Errorf("Glob missing .go files: %s", res.Output)
	}
	if strings.Contains(res.Output, "c.txt") {
		t.Error("Glob should not include .txt files")
	}
}

// TestGrepTool: grep 正则搜索
func TestGrepTool(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package main\nfunc foo() {}\nvar bar = 1\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "b.go"), []byte("package main\nfunc baz() {}\n"), 0o644)
	tool := &GrepTool{}
	res := tool.Execute(context.Background(), map[string]any{"pattern": "func \\w+", "path": tmp, "include": "*.go"})
	if res.IsError {
		t.Errorf("Grep failed: %s", res.Output)
	}
	if !strings.Contains(res.Output, "foo") || !strings.Contains(res.Output, "baz") {
		t.Errorf("Grep missing matches: %s", res.Output)
	}
}

// TestBashToolExitCodeInterpretation: grep 退出码 1（无匹配）不算错误
func TestBashToolExitCodeInterpretation(t *testing.T) {
	if interpretExitCode("grep pattern missing", 1) {
		t.Error("grep exit 1 should not be error (no matches)")
	}
	if !interpretExitCode("grep pattern", 2) {
		t.Error("grep exit 2 should be error")
	}
	if !interpretExitCode("ls", 1) {
		t.Error("ls exit 1 should be error")
	}
	if interpretExitCode("echo hi", 0) {
		t.Error("exit 0 should not be error")
	}
}

// TestBashToolCompoundCommand: 复合命令拆分 - 在 permissions 包测试
func TestBashToolCompoundCommand(t *testing.T) {
	t.Skip("splitCompoundCommand is in permissions package")
}

// TestToolRegistry: 工具注册和获取
func TestToolRegistry(t *testing.T) {
	reg := NewRegistry()
	tool := &ReadFileTool{FileStateCache: NewFileStateCache()}
	reg.Register(tool)
	if reg.Get("ReadFile") == nil {
		t.Error("Get failed to find registered tool")
	}
	if len(reg.ListTools()) != 1 {
		t.Errorf("ListTools = %d, want 1", len(reg.ListTools()))
	}
}

// TestToolSchemaAdaptation: Schema 按协议适配
func TestToolSchemaAdaptation(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&ReadFileTool{FileStateCache: NewFileStateCache()})
	// anthropic 协议
	anthropicSchemas := reg.GetAllSchemas("anthropic")
	if len(anthropicSchemas) != 1 {
		t.Fatalf("anthropic: expected 1 schema, got %d", len(anthropicSchemas))
	}
	if _, ok := anthropicSchemas[0]["input_schema"]; !ok {
		t.Error("anthropic schema should have input_schema")
	}
	// openai 协议
	openaiSchemas := reg.GetAllSchemas("openai")
	if len(openaiSchemas) != 1 {
		t.Fatalf("openai: expected 1 schema, got %d", len(openaiSchemas))
	}
	if _, ok := openaiSchemas[0]["parameters"]; !ok {
		t.Error("openai schema should have parameters")
	}
}

// --- 工具性能基准 ---

func BenchmarkReadFileTool(b *testing.B) {
	tmp := b.TempDir()
	path := filepath.Join(tmp, "test.txt")
	content := strings.Repeat("line content here\n", 200)
	os.WriteFile(path, []byte(content), 0o644)
	tool := &ReadFileTool{FileStateCache: NewFileStateCache()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(context.Background(), map[string]any{"file_path": path})
	}
}

func BenchmarkWriteFileTool(b *testing.B) {
	tmp := b.TempDir()
	path := filepath.Join(tmp, "out.txt")
	content := strings.Repeat("new content line\n", 200)
	tool := &WriteFileTool{FileStateCache: NewFileStateCache()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(context.Background(), map[string]any{"file_path": path, "content": content})
	}
}

func BenchmarkEditFileTool(b *testing.B) {
	tmp := b.TempDir()
	path := filepath.Join(tmp, "edit.txt")
	content := "package main\n\nfunc target() {\n\treturn\n}\n"
	fsc := NewFileStateCache()
	tool := &EditFileTool{FileStateCache: fsc}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		os.WriteFile(path, []byte(content), 0o644)
		info, _ := os.Stat(path)
		fsc.Record(path, content, info.ModTime().UnixMilli())
		b.StartTimer()
		tool.Execute(context.Background(), map[string]any{
			"file_path":  path,
			"old_string": "func target() {",
			"new_string": "func renamed() {",
		})
	}
}

func BenchmarkGlobTool(b *testing.B) {
	tmp := b.TempDir()
	for i := 0; i < 100; i++ {
		dir := filepath.Join(tmp, "dir"+string(rune('a'+i%10)))
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "file.go"), []byte("x"), 0o644)
	}
	tool := &GlobTool{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(context.Background(), map[string]any{"pattern": "**/*.go", "path": tmp})
	}
}

func BenchmarkGrepTool(b *testing.B) {
	tmp := b.TempDir()
	for i := 0; i < 50; i++ {
		path := filepath.Join(tmp, "file.go")
		content := strings.Repeat("func handler() { return }\n", 20)
		os.WriteFile(path, []byte(content), 0o644)
	}
	tool := &GrepTool{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(context.Background(), map[string]any{"pattern": "func", "path": tmp, "include": "*.go"})
	}
}

func BenchmarkToolRegistryGetAllSchemas(b *testing.B) {
	reg := NewRegistry()
	fsc := NewFileStateCache()
	reg.Register(&ReadFileTool{FileStateCache: fsc})
	reg.Register(&WriteFileTool{FileStateCache: fsc})
	reg.Register(&EditFileTool{FileStateCache: fsc})
	reg.Register(&GlobTool{})
	reg.Register(&GrepTool{})
	reg.Register(&BashTool{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.GetAllSchemas("anthropic")
	}
}
