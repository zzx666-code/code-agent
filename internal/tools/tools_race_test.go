package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestFileStateCacheConcurrentRecordAndCheck: 并发 Record + Check + Update，-race 下不应报错
func TestFileStateCacheConcurrentRecordAndCheck(t *testing.T) {
	tmp := t.TempDir()
	fsc := NewFileStateCache()

	// 预创建 50 个文件
	paths := make([]string, 50)
	for i := range paths {
		paths[i] = filepath.Join(tmp, fmt.Sprintf("file_%d.go", i))
		os.WriteFile(paths[i], []byte("content"), 0o644)
		info, _ := os.Stat(paths[i])
		fsc.Record(paths[i], "content", info.ModTime().UnixMilli())
	}

	var wg sync.WaitGroup

	// 5 个 goroutine 并发 Record（写）
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				path := filepath.Join(tmp, fmt.Sprintf("new_%d_%d.go", id, i))
				os.WriteFile(path, []byte("new"), 0o644)
				info, _ := os.Stat(path)
				fsc.Record(path, "new", info.ModTime().UnixMilli())
			}
		}(w)
	}

	// 10 个 goroutine 并发 Check（读）
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				path := paths[i%len(paths)]
				fsc.Check(path)
			}
		}()
	}

	// 3 个 goroutine 并发 Update（写）
	for u := 0; u < 3; u++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				path := filepath.Join(tmp, fmt.Sprintf("upd_%d_%d.go", id, i))
				os.WriteFile(path, []byte("updated"), 0o644)
				fsc.Update(path, "updated")
			}
		}(u)
	}

	wg.Wait()
	// 不 panic、不 race 即通过
}

// TestFileStateCacheConcurrentSameFile: 高并发对同一文件 Record+Check
func TestFileStateCacheConcurrentSameFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "hot.go")
	os.WriteFile(path, []byte("hot content"), 0o644)
	fsc := NewFileStateCache()
	info, _ := os.Stat(path)
	fsc.Record(path, "hot content", info.ModTime().UnixMilli())

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				fsc.Check(path)
				fsc.Record(path, fmt.Sprintf("content-%d-%d", id, j), info.ModTime().UnixMilli())
				fsc.Check(path)
			}
		}(i)
	}
	wg.Wait()
}
