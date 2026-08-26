package taskstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Mode string

const (
	ModeReAct          Mode = "react"
	ModePlanAndExecute Mode = "plan_and_execute"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusFailed    Status = "failed"
	StatusCompleted Status = "completed"
)

type Complexity struct {
	Files       int  `json:"files"`
	Steps       int  `json:"steps"`
	Commands    int  `json:"commands"`
	NeedsPlan   bool `json:"needs_plan"`
	HasRefactor bool `json:"has_refactor"`
}

type State struct {
	ID           string     `json:"id"`
	Task         string     `json:"task"`
	Mode         Mode       `json:"mode"`
	Status       Status     `json:"status"`
	Attempt      int        `json:"attempt"`
	Step         int        `json:"step"`
	LastError    string     `json:"last_error,omitempty"`
	Checkpoint   string     `json:"checkpoint,omitempty"`
	Complexity   Complexity `json:"complexity"`
	StartedAt    time.Time  `json:"started_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CompletedAt  time.Time  `json:"completed_at,omitempty"`
	BeforeTokens int        `json:"before_tokens,omitempty"`
	AfterTokens  int        `json:"after_tokens,omitempty"`
	MemoryKeys   []string   `json:"memory_keys,omitempty"`

	mu sync.Mutex
}

func Analyze(task string) Complexity {
	lower := strings.ToLower(task)
	words := strings.Fields(lower)
	c := Complexity{Steps: 1}
	for _, word := range words {
		switch {
		case strings.Contains(word, "file"), strings.Contains(word, "文件"), strings.Contains(word, "module"), strings.Contains(word, "目录"):
			c.Files++
		case strings.Contains(word, "refactor"), strings.Contains(word, "重构"), strings.Contains(word, "migrate"), strings.Contains(word, "迁移"):
			c.HasRefactor = true
		case strings.Contains(word, "command"), strings.Contains(word, "命令"), strings.Contains(word, "test"), strings.Contains(word, "测试"), strings.Contains(word, "build"), strings.Contains(word, "构建"):
			c.Commands++
		}
	}
	for _, marker := range []string{"then", "and", "after", "然后", "并且", "之后", "步骤", "step"} {
		c.Steps += strings.Count(lower, marker)
	}
	c.NeedsPlan = c.HasRefactor || c.Files >= 3 || c.Commands >= 2 || c.Steps >= 3 || len(words) >= 80
	return c
}

func ChooseMode(task string) (Mode, Complexity) {
	c := Analyze(task)
	if c.NeedsPlan {
		return ModePlanAndExecute, c
	}
	return ModeReAct, c
}

func New(id, task string) *State {
	mode, complexity := ChooseMode(task)
	now := time.Now().UTC()
	return &State{ID: id, Task: task, Mode: mode, Status: StatusPending, Complexity: complexity, UpdatedAt: now}
}

func (s *State) transition(status Status, errText string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	s.LastError = errText
	s.UpdatedAt = time.Now().UTC()
	if status == StatusRunning {
		if s.StartedAt.IsZero() {
			s.StartedAt = s.UpdatedAt
		}
		s.Attempt++
	}
	if status == StatusCompleted {
		s.CompletedAt = s.UpdatedAt
	}
}

func (s *State) Start()              { s.transition(StatusRunning, "") }
func (s *State) Pause(reason string) { s.transition(StatusPaused, reason) }
func (s *State) Fail(reason string)  { s.transition(StatusFailed, reason) }
func (s *State) Complete()           { s.transition(StatusCompleted, "") }
func (s *State) Retry()              { s.transition(StatusRunning, "") }

func (s *State) SetCheckpoint(step int, checkpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Step = step
	s.Checkpoint = checkpoint
	s.UpdatedAt = time.Now().UTC()
}

func Path(workDir, id string) string {
	return filepath.Join(workDir, ".mewcode", "tasks", id+".state.json")
}

func Save(workDir string, s *State) error {
	if s == nil || strings.TrimSpace(s.ID) == "" {
		return errors.New("task state requires an id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(Path(workDir, s.ID)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path(workDir, s.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path(workDir, s.ID))
}

func Load(workDir, id string) (*State, error) {
	data, err := os.ReadFile(Path(workDir, id))
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.ID == "" {
		s.ID = id
	}
	return &s, nil
}
