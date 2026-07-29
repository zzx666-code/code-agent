package compact

import (
	"fmt"
	"sync"
	"testing"
)

// TestRecoveryStateConcurrentRecord: 并发 RecordFileRead + RecordSkillInvocation，-race 下不应报错
func TestRecoveryStateConcurrentRecord(t *testing.T) {
	rs := NewRecoveryState()
	var wg sync.WaitGroup

	// 10 个 goroutine 并发写 file reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				path := fmt.Sprintf("/tmp/file_%d_%d.go", id, j)
				rs.RecordFileRead(path, "content")
			}
		}(i)
	}

	// 5 个 goroutine 并发写 skill invocations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				name := fmt.Sprintf("skill-%d", id%3) // 故意冲突，测试覆盖
				rs.RecordSkillInvocation(name, "body")
			}
		}(i)
	}

	// 3 个 goroutine 并发读（snapshotFiles / snapshotSkills）
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				rs.snapshotFiles(10)
				rs.snapshotSkills()
			}
		}()
	}

	wg.Wait()
	// 不 panic、不 race 即通过
}

// TestRecoveryStateConcurrentReadWrite: 并发读 snapshot + 写 record
func TestRecoveryStateConcurrentReadWrite(t *testing.T) {
	rs := NewRecoveryState()
	// 预填一些数据
	for i := 0; i < 20; i++ {
		rs.RecordFileRead(fmt.Sprintf("/tmp/init_%d.go", i), "init")
	}

	var wg sync.WaitGroup
	// 持续写
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			rs.RecordFileRead(fmt.Sprintf("/tmp/write_%d.go", i), "data")
		}
	}()

	// 持续读
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				rs.snapshotFiles(10)
			}
		}()
	}

	wg.Wait()
}
