//go:build e2e

package rag

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mewcode/internal/config"
)

// 这组测试需要真实 embedding API，用 -tags e2e 运行：
//   go test ./internal/rag/ -tags e2e -v -timeout 600s
//
// 会读取 .mewcode/config.yaml 中的 embedding 配置（embedding_model / embedding_url / embedding_api_key）。

func loadEmbedderFromConfig(t *testing.T) *Embedder {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// 从项目根目录找 config.yaml（测试运行目录是 internal/rag/）
	cfgPath := filepath.Join(wd, "..", "..", ".mewcode", "config.yaml")
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Skipf("skip: cannot load config: %v", err)
	}
	for _, p := range cfg.Providers {
		if p.EmbeddingModel != "" {
			emb, err := NewEmbedder(&p)
			if err != nil {
				t.Fatalf("NewEmbedder: %v", err)
			}
			return emb
		}
	}
	t.Skip("skip: no provider with embedding_model configured")
	return nil
}

// ---------- 测试语料：模拟代码库的多个主题 ----------

type corpusDoc struct {
	path    string
	content string
	topic   string // 人工标注的 ground-truth 主题
}

func buildTestCorpus() []corpusDoc {
	return []corpusDoc{
		// 主题: auth
		{path: "auth/login.go", topic: "auth", content: `package auth

import "crypto/sha256"

func Login(username, password string) (string, error) {
	hashed := sha256.Sum256([]byte(password))
	token, err := generateToken(username, string(hashed[:]))
	if err != nil {
		return "", err
	}
	return token, nil
}

func generateToken(user string, pwdHash string) (string, error) {
	return user + ":" + pwdHash, nil
}`},
		{path: "auth/session.go", topic: "auth", content: `package auth

import "time"

type Session struct {
	UserID    string
	Token     string
	ExpiresAt time.Time
}

func ValidateSession(token string) (*Session, error) {
	s, ok := sessions[token]
	if !ok {
		return nil, ErrInvalidToken
	}
	if time.Now().After(s.ExpiresAt) {
		delete(sessions, token)
		return nil, ErrExpired
	}
	return s, nil
}`},
		{path: "auth/jwt.go", topic: "auth", content: `package auth

import "encoding/base64"
import "encoding/json"

func EncodeJWT(payload map[string]any) string {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(h) + "." +
		base64.RawURLEncoding.EncodeToString(p)
}`},

		// 主题: database
		{path: "db/conn.go", topic: "database", content: `package db

import "database/sql"
import _ "github.com/lib/pq"

func Connect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}`},
		{path: "db/query.go", topic: "database", content: `package db

import "database/sql"

func QueryUsers(db *sql.DB) ([]User, error) {
	rows, err := db.Query("SELECT id, name, email FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}`},
		{path: "db/migrate.go", topic: "database", content: `package db

import "database/sql"

func Migrate(db *sql.DB) error {
	queries := []string{
		"CREATE TABLE IF NOT EXISTS users (id SERIAL PRIMARY KEY, name TEXT, email TEXT)",
		"CREATE TABLE IF NOT EXISTS orders (id SERIAL PRIMARY KEY, user_id INT REFERENCES users(id))",
	}
	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}`},

		// 主题: logging
		{path: "log/logger.go", topic: "logging", content: `package log

import "fmt"
import "os"
import "time"

type Logger struct {
	level Level
	out   *os.File
}

func (l *Logger) Info(msg string) {
	if l.level <= InfoLevel {
		fmt.Fprintf(l.out, "[INFO] %s %s\n", time.Now().Format(time.RFC3339), msg)
	}
}

func (l *Logger) Error(msg string) {
	if l.level <= ErrorLevel {
		fmt.Fprintf(l.out, "[ERROR] %s %s\n", time.Now().Format(time.RFC3339), msg)
	}
}`},
		{path: "log/rotate.go", topic: "logging", content: `package log

import "os"
import "fmt"

func RotateLogFile(path string, maxSize int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() < maxSize {
		return nil
	}
	backup := fmt.Sprintf("%s.%d", path, info.ModTime().Unix())
	return os.Rename(path, backup)
}`},

		// 主题: network
		{path: "net/http_client.go", topic: "network", content: `package net

import "net/http"
import "context"
import "time"

type Client struct {
	httpClient *http.Client
	timeout    time.Duration
}

func (c *Client) Get(ctx context.Context, url string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}`},
		{path: "net/websocket.go", topic: "network", content: `package net

import "github.com/gorilla/websocket"

type WSConn struct {
	conn *websocket.Conn
}

func (w *WSConn) Send(msg string) error {
	return w.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

func (w *WSConn) Recv() (string, error) {
	_, msg, err := w.conn.ReadMessage()
	return string(msg), err
}`},

		// 主题: cache
		{path: "cache/redis.go", topic: "cache", content: `package cache

import "context"
import "github.com/redis/go-redis/v9"

type RedisCache struct {
	client *redis.Client
}

func (r *RedisCache) Set(ctx context.Context, key, val string) error {
	return r.client.Set(ctx, key, val, 0).Err()
}

func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}`},
		{path: "cache/memory.go", topic: "cache", content: `package cache

import "sync"

type MemoryCache struct {
	mu    sync.RWMutex
	store map[string]string
}

func (m *MemoryCache) Set(key, val string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[key] = val
}

func (m *MemoryCache) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.store[key]
	return v, ok
}`},
		{path: "cache/lru.go", topic: "cache", content: `package cache

type LRUNode struct {
	key  string
	val  string
	prev *LRUNode
	next *LRUNode
}

type LRUCache struct {
	capacity int
	head     *LRUNode
	tail     *LRUNode
	store    map[string]*LRUNode
}

func (l *LRUCache) Get(key string) (string, bool) {
	node, ok := l.store[key]
	if !ok {
		return "", false
	}
	l.moveToFront(node)
	return node.val, true
}`},
	}
}

// ---------- 测试查询：每条带人工标注的期望主题 ----------

type evalQuery struct {
	query         string
	expectedTopic string
}

func buildEvalQueries() []evalQuery {
	return []evalQuery{
		{"用户登录认证怎么做", "auth"},
		{"password 验证和 token 生成", "auth"},
		{"JWT 编码", "auth"},
		{"session 过期检查", "auth"},
		{"数据库连接", "database"},
		{"查询用户列表", "database"},
		{"SQL 建表迁移", "database"},
		{"日志记录器", "logging"},
		{"日志文件轮转", "logging"},
		{"HTTP 客户端请求", "network"},
		{"WebSocket 收发消息", "network"},
		{"Redis 缓存读写", "cache"},
		{"内存缓存 map", "cache"},
		{"LRU 缓存淘汰", "cache"},
		{"token 验证", "auth"}, // 跨主题：也期望 auth
	}
}

// ---------- 指标计算 ----------

// Recall@K: top-K 结果中包含至少一个相关（同主题）chunk 的比例
func recallAtK(results []SearchResult, expectedTopic string, allDocs []corpusDoc, k int) float64 {
	if k > len(results) {
		k = len(results)
	}
	relevantTotal := 0
	for _, d := range allDocs {
		if d.topic == expectedTopic {
			relevantTotal++
		}
	}
	if relevantTotal == 0 {
		return 0
	}
	retrievedRelevant := 0
	for i := 0; i < k && i < len(results); i++ {
		doc := findDocByPath(allDocs, results[i].FilePath)
		if doc != nil && doc.topic == expectedTopic {
			retrievedRelevant++
		}
	}
	return float64(retrievedRelevant) / float64(relevantTotal)
}

// Precision@K: top-K 结果中相关 chunk 的比例
func precisionAtK(results []SearchResult, expectedTopic string, allDocs []corpusDoc, k int) float64 {
	if k > len(results) {
		k = len(results)
	}
	if k == 0 {
		return 0
	}
	relevant := 0
	for i := 0; i < k && i < len(results); i++ {
		doc := findDocByPath(allDocs, results[i].FilePath)
		if doc != nil && doc.topic == expectedTopic {
			relevant++
		}
	}
	return float64(relevant) / float64(k)
}

// MRR: Mean Reciprocal Rank，第一个相关结果排名的倒数的均值
func reciprocalRank(results []SearchResult, expectedTopic string, allDocs []corpusDoc) float64 {
	for i, r := range results {
		doc := findDocByPath(allDocs, r.FilePath)
		if doc != nil && doc.topic == expectedTopic {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCG@K: 归一化折损累积增益，相关结果排名越靠前得分越高
func ndcgAtK(results []SearchResult, expectedTopic string, allDocs []corpusDoc, k int) float64 {
	if k > len(results) {
		k = len(results)
	}
	// DCG
	var dcg float64
	for i := 0; i < k && i < len(results); i++ {
		doc := findDocByPath(allDocs, results[i].FilePath)
		rel := 0.0
		if doc != nil && doc.topic == expectedTopic {
			rel = 1.0
		}
		dcg += rel / math.Log2(float64(i+2))
	}
	// IDCG: 理想情况下所有相关结果都排前面
	relevantTotal := 0
	for _, d := range allDocs {
		if d.topic == expectedTopic {
			relevantTotal++
		}
	}
	var idcg float64
	for i := 0; i < relevantTotal && i < k; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func findDocByPath(docs []corpusDoc, path string) *corpusDoc {
	// 匹配末尾路径（store 里存的是绝对路径，corpus 里是相对路径）
	for i := range docs {
		if strings.HasSuffix(path, docs[i].path) || docs[i].path == filepath.Base(path) {
			return &docs[i]
		}
	}
	return nil
}

// ---------- 集成测试：端到端 RAG 效果评估 ----------

func TestRAGEndToEndEvaluation(t *testing.T) {
	embedder := loadEmbedderFromConfig(t)
	ctx := context.Background()

	docs := buildTestCorpus()
	queries := buildEvalQueries()

	// 1. 构建索引：把每个文档作为一个 chunk
	t.Log("正在构建索引（调用真实 embedding API）...")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	var chunks []Chunk
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.content
	}
	embeddings, dim, err := embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed corpus: %v", err)
	}
	t.Logf("索引完成: %d 个文档, 向量维度 %d", len(docs), dim)
	store.SetModel(embedder.Model(), dim)

	for i, d := range docs {
		chunks = append(chunks, Chunk{
			FilePath:  d.path,
			StartLine: 1,
			EndLine:   strings.Count(d.content, "\n") + 1,
			ChunkType: "code",
			Language:  "go",
			Content:   d.content,
			Embedding: embeddings[i],
		})
	}
	if err := store.InsertChunks(ctx, chunks); err != nil {
		t.Fatalf("InsertChunks: %v", err)
	}

	// 2. 逐条查询并计算指标
	type queryResult struct {
		query         string
		expectedTopic string
		results       []SearchResult
		recall        float64
		precision     float64
		mrr           float64
		ndcg          float64
		latency       time.Duration
	}

	var allResults []queryResult
	var totalRecall, totalPrecision, totalMRR, totalNDCG float64
	var totalLatency time.Duration

	for _, q := range queries {
		start := time.Now()
		queryVec, _, err := embedder.EmbedOne(ctx, q.query)
		if err != nil {
			t.Fatalf("Embed query %q: %v", q.query, err)
		}
		results, err := store.Search(ctx, queryVec, 5)
		latency := time.Since(start)
		if err != nil {
			t.Fatalf("Search %q: %v", q.query, err)
		}

		qr := queryResult{
			query:         q.query,
			expectedTopic: q.expectedTopic,
			results:       results,
			recall:        recallAtK(results, q.expectedTopic, docs, 5),
			precision:     precisionAtK(results, q.expectedTopic, docs, 5),
			mrr:           reciprocalRank(results, q.expectedTopic, docs),
			ndcg:          ndcgAtK(results, q.expectedTopic, docs, 5),
			latency:       latency,
		}
		allResults = append(allResults, qr)
		totalRecall += qr.recall
		totalPrecision += qr.precision
		totalMRR += qr.mrr
		totalNDCG += qr.ndcg
		totalLatency += latency

		t.Logf("  [%.1fms] Q: %-28s | Recall=%.2f Prec=%.2f MRR=%.2f NDCG=%.2f | top1=%s",
			float64(latency.Microseconds())/1000, q.query,
			qr.recall, qr.precision, qr.mrr, qr.ndcg,
			top1Path(results))
	}

	n := float64(len(queries))

	// 3. 输出汇总报告
	t.Log("")
	t.Log("========== RAG 性能评估报告 ==========")
	t.Logf("语料库: %d 个文档, %d 个主题", len(docs), countTopics(docs))
	t.Logf("查询集: %d 条", len(queries))
	t.Logf("Embedding 模型: %s (dim=%d)", embedder.Model(), dim)
	t.Logf("Top-K: 5")
	t.Log("")
	t.Logf("Recall@5    (召回率):   %.4f  (%.1f%%)", totalRecall/n, totalRecall/n*100)
	t.Logf("Precision@5 (准确率):   %.4f  (%.1f%%)", totalPrecision/n, totalPrecision/n*100)
	t.Logf("MRR         (平均倒数排名): %.4f", totalMRR/n)
	t.Logf("NDCG@5      (归一化折损增益): %.4f", totalNDCG/n)
	t.Logf("Avg Latency (平均检索延迟): %.2f ms", float64(totalLatency.Microseconds())/1000/n)
	t.Log("======================================")

	// 4. 断言：核心指标达标阈值（简历可用数据）
	avgRecall := totalRecall / n
	avgPrecision := totalPrecision / n
	avgMRR := totalMRR / n

	if avgRecall < 0.5 {
		t.Errorf("Recall@5 = %.2f, 期望 >= 0.50", avgRecall)
	}
	if avgPrecision < 0.5 {
		t.Errorf("Precision@5 = %.2f, 期望 >= 0.50", avgPrecision)
	}
	if avgMRR < 0.5 {
		t.Errorf("MRR = %.2f, 期望 >= 0.50", avgMRR)
	}
}

// ---------- 性能基准：不同规模下的检索延迟 ----------

func TestRAGSearchLatency(t *testing.T) {
	embedder := loadEmbedderFromConfig(t)
	ctx := context.Background()

	// 用真实 embedding 构建不同规模的索引
	scales := []int{10, 50, 100, 200}

	t.Log("========== RAG 检索延迟基准 ==========")
	t.Log("规模(chunk数) | 索引耗时 | 检索耗时(p50) | 检索耗时(p99) | 吞吐(QPS)")
	t.Log("-----------------------------------------------------------")

	for _, n := range scales {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}

		// 生成 n 个 chunk 的文本
		texts := make([]string, n)
		for i := 0; i < n; i++ {
			texts[i] = fmt.Sprintf("package main\nfunc function%d() {\n\t// this is function number %d\n\treturn\n}\n", i, i)
		}
		// 批量 embed
		indexStart := time.Now()
		embeddings, dim, err := embedder.Embed(ctx, texts)
		if err != nil {
			store.Close()
			t.Fatalf("Embed at scale %d: %v", n, err)
		}
		indexTime := time.Since(indexStart)

		store.SetModel(embedder.Model(), dim)
		var chunks []Chunk
		for i, txt := range texts {
			chunks = append(chunks, Chunk{
				FilePath:  fmt.Sprintf("file%d.go", i),
				StartLine: 1, EndLine: 5,
				ChunkType: "code", Language: "go",
				Content:   txt,
				Embedding: embeddings[i],
			})
		}
		store.InsertChunks(ctx, chunks)

		// 跑 20 次查询取分位
		const queryCount = 20
		var latencies []time.Duration
		for i := 0; i < queryCount; i++ {
			queryVec, _, err := embedder.EmbedOne(ctx, fmt.Sprintf("function %d", i%n))
			if err != nil {
				store.Close()
				t.Fatalf("EmbedOne: %v", err)
			}
			start := time.Now()
			store.Search(ctx, queryVec, 5)
			latencies = append(latencies, time.Since(start))
			// 查询 embedding 也要限速
			if i < queryCount-1 {
				time.Sleep(800 * time.Millisecond)
			}
		}
		store.Close()

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		p50 := latencies[len(latencies)*50/100]
		p99 := latencies[len(latencies)*99/100]
		avgMs := float64(p50.Microseconds()) / 1000
		qps := 1000.0 / avgMs

		t.Logf("%4d         | %6.0fms | %10.2fms | %10.2fms | %8.0f",
			n,
			float64(indexTime.Microseconds())/1000,
			float64(p50.Microseconds())/1000,
			float64(p99.Microseconds())/1000,
			qps)
	}
	t.Log("======================================")
}

// ---------- 辅助 ----------

func top1Path(results []SearchResult) string {
	if len(results) == 0 {
		return "(none)"
	}
	return filepath.Base(results[0].FilePath)
}

func countTopics(docs []corpusDoc) int {
	seen := map[string]bool{}
	for _, d := range docs {
		seen[d.topic] = true
	}
	return len(seen)
}
