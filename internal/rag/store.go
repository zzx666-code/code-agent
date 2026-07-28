package rag

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

type Chunk struct {
	FilePath  string
	StartLine int
	EndLine   int
	ChunkType string
	Language  string
	Content   string
	Embedding []float32
}

type Store struct {
	dbPath string
	mu     sync.Mutex
	db     *sql.DB
	dim    int
}

func NewStore(baseDir string) (*Store, error) {
	ragDir := filepath.Join(baseDir, ".mewcode", "rag")
	if err := os.MkdirAll(ragDir, 0o755); err != nil {
		return nil, fmt.Errorf("create rag dir: %w", err)
	}
	dbPath := filepath.Join(ragDir, "rag.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{dbPath: dbPath, db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		file_path TEXT NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		chunk_type TEXT NOT NULL,
		language TEXT NOT NULL,
		content TEXT NOT NULL,
		embedding BLOB NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	_, err = s.db.Exec(`CREATE TABLE IF NOT EXISTS meta (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create meta table: %w", err)
	}
	return nil
}

func (s *Store) SetModel(model string, dim int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dim = dim
	_, err := s.db.Exec(
		`INSERT INTO meta(key, value) VALUES('embedding_model', ?), ('embedding_dim', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		model, fmt.Sprintf("%d", dim))
	return err
}

func (s *Store) GetModel() (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dim > 0 {
		var model string
		s.db.QueryRow(`SELECT value FROM meta WHERE key='embedding_model'`).Scan(&model)
		return model, s.dim
	}
	var modelStr, dimStr string
	s.db.QueryRow(`SELECT value FROM meta WHERE key='embedding_model'`).Scan(&modelStr)
	s.db.QueryRow(`SELECT value FROM meta WHERE key='embedding_dim'`).Scan(&dimStr)
	if modelStr == "" {
		return "", 0
	}
	fmt.Sscanf(dimStr, "%d", &s.dim)
	return modelStr, s.dim
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM chunks`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM meta`)
	s.dim = 0
	return err
}

func (s *Store) InsertChunks(ctx context.Context, chunks []Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range chunks {
		blob := encodeFloats(c.Embedding)
		_, err := tx.ExecContext(ctx,
			`INSERT INTO chunks(file_path, start_line, end_line, chunk_type, language, content, embedding)
			 VALUES(?,?,?,?,?,?,?)`,
			c.FilePath, c.StartLine, c.EndLine, c.ChunkType, c.Language, c.Content, blob)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

type SearchResult struct {
	FilePath  string
	StartLine int
	EndLine   int
	Content   string
	Score     float32
}

func (s *Store) Search(ctx context.Context, queryVec []float32, topK int) ([]SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		`SELECT file_path, start_line, end_line, content, embedding FROM chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type entry struct {
		r   SearchResult
		vec []float32
	}
	var all []entry
	for rows.Next() {
		var fp, content string
		var sl, el int
		var blob []byte
		if err := rows.Scan(&fp, &sl, &el, &content, &blob); err != nil {
			return nil, err
		}
		vec, err := decodeFloats(blob)
		if err != nil {
			continue
		}
		all = append(all, entry{
			r:   SearchResult{FilePath: fp, StartLine: sl, EndLine: el, Content: content},
			vec: vec,
		})
	}
	if len(all) == 0 {
		return nil, nil
	}
	for i := range all {
		all[i].r.Score = cosineSim(queryVec, all[i].vec)
	}
	for i := 0; i < len(all) && i < topK; i++ {
		maxIdx := i
		for j := i + 1; j < len(all); j++ {
			if all[j].r.Score > all[maxIdx].r.Score {
				maxIdx = j
			}
		}
		all[i], all[maxIdx] = all[maxIdx], all[i]
	}
	results := make([]SearchResult, 0, topK)
	for i := 0; i < topK && i < len(all); i++ {
		results = append(results, all[i].r)
	}
	return results, nil
}

type Stats struct {
	ChunkCount int
	FileCount  int
	DBSize     int64
	Model      string
	Dim        int
}

func (s *Store) Stats() (*Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var chunkCount, fileCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&chunkCount)
	s.db.QueryRow(`SELECT COUNT(DISTINCT file_path) FROM chunks`).Scan(&fileCount)
	var modelStr, dimStr string
	s.db.QueryRow(`SELECT value FROM meta WHERE key='embedding_model'`).Scan(&modelStr)
	s.db.QueryRow(`SELECT value FROM meta WHERE key='embedding_dim'`).Scan(&dimStr)
	var size int64
	if info, err := os.Stat(s.dbPath); err == nil {
		size = info.Size()
	}
	dim := 0
	fmt.Sscanf(dimStr, "%d", &dim)
	return &Stats{
		ChunkCount: chunkCount,
		FileCount:  fileCount,
		DBSize:     size,
		Model:      modelStr,
		Dim:        dim,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func encodeFloats(fs []float32) []byte {
	buf := make([]byte, 4*len(fs))
	for i, f := range fs {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func decodeFloats(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("invalid blob length")
	}
	fs := make([]float32, len(buf)/4)
	for i := range fs {
		bits := binary.LittleEndian.Uint32(buf[i*4:])
		fs[i] = math.Float32frombits(bits)
	}
	return fs, nil
}

func cosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(sqrt32(na)*sqrt32(nb))
}

func sqrt32(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
