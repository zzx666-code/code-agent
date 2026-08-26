// Package retrieval provides a deterministic, offline code retrieval index.
// It complements vector RAG with symbol-aware and path-aware search.
package retrieval

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type SearchMode string

const (
	ModeKeyword SearchMode = "keyword"
	ModeSymbol  SearchMode = "symbol"
	ModeHybrid  SearchMode = "hybrid"
)

type SearchOptions struct {
	Mode         SearchMode
	TopK         int
	ContextLines int
}

type Symbol struct {
	Name, Kind, FilePath, Parent, Signature, Content string
	StartLine, EndLine                               int
}

type Document struct {
	Path, Content string
	Lines         []string
	Symbols       []*Symbol
}

type Directory struct {
	Path     string
	Files    []string
	Children []string
}

type DirectoryResult struct {
	Directory Directory
	Score     float64
}

type Result struct {
	FilePath           string
	StartLine, EndLine int
	Score              float64
	Symbol             *Symbol
	Content, Context   string
}

type Index struct {
	root    string
	docs    []*Document
	symbols []*Symbol
	dirs    []Directory
}

func Build(root string) (*Index, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	idx := &Index{root: root}
	if !st.IsDir() {
		if err := idx.addFile(root); err != nil {
			return nil, err
		}
		idx.buildDirs()
		return idx, nil
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirectory(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipFile(info.Name()) {
			return nil
		}
		_ = idx.addFile(path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx.buildDirs()
	return idx, nil
}

func (i *Index) Symbols() []*Symbol     { return append([]*Symbol(nil), i.symbols...) }
func (i *Index) Documents() []*Document { return append([]*Document(nil), i.docs...) }
func (i *Index) Directories() []Directory {
	out := make([]Directory, len(i.dirs))
	copy(out, i.dirs)
	return out
}

// SearchDirectories ranks directories by path and contained file names. It is
// intentionally lexical and deterministic, so callers can use it even when
// embeddings are unavailable.
func (i *Index) SearchDirectories(query string, topK int) []DirectoryResult {
	if topK <= 0 {
		topK = 5
	}
	terms := tokenize(query)
	out := make([]DirectoryResult, 0, len(i.dirs))
	for _, d := range i.dirs {
		text := d.Path + " " + strings.Join(d.Files, " ") + " " + strings.Join(d.Children, " ")
		if score := keywordScore(terms, text); score > 0 {
			out = append(out, DirectoryResult{Directory: d, Score: score})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score == out[b].Score {
			return out[a].Directory.Path < out[b].Directory.Path
		}
		return out[a].Score > out[b].Score
	})
	if len(out) > topK {
		out = out[:topK]
	}
	return out
}

func (i *Index) addFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 || bytesBinary(data) {
		return nil
	}
	content := string(data)
	doc := &Document{Path: path, Content: content, Lines: strings.Split(content, "\n")}
	if strings.EqualFold(filepath.Ext(path), ".go") {
		doc.Symbols = parseGoSymbols(path, content, i.root)
	} else {
		doc.Symbols = parseGenericSymbols(path, content, i.root)
	}
	i.docs = append(i.docs, doc)
	i.symbols = append(i.symbols, doc.Symbols...)
	return nil
}

func (i *Index) buildDirs() {
	dirs := map[string]*Directory{".": {Path: "."}}
	for _, d := range i.docs {
		rel, _ := filepath.Rel(i.root, d.Path)
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dirs["."].Files = append(dirs["."].Files, rel)
			continue
		}
		parts := strings.Split(dir, "/")
		parent := "."
		for n := range parts {
			current := strings.Join(parts[:n+1], "/")
			if _, ok := dirs[current]; !ok {
				dirs[current] = &Directory{Path: current}
			}
			if parent != "." && !contains(dirs[parent].Children, current) {
				dirs[parent].Children = append(dirs[parent].Children, current)
			}
			if parent == "." && !contains(dirs[parent].Children, current) {
				dirs[parent].Children = append(dirs[parent].Children, current)
			}
			parent = current
		}
		dirs[dir].Files = append(dirs[dir].Files, rel)
	}
	keys := make([]string, 0, len(dirs))
	for k := range dirs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sort.Strings(dirs[k].Files)
		sort.Strings(dirs[k].Children)
		i.dirs = append(i.dirs, *dirs[k])
	}
}

func (i *Index) Search(query string, opts SearchOptions) []Result {
	if opts.TopK <= 0 {
		opts.TopK = 5
	}
	if opts.Mode == "" {
		opts.Mode = ModeHybrid
	}
	qTokens := tokenize(query)
	var out []Result
	for _, d := range i.docs {
		rel, _ := filepath.Rel(i.root, d.Path)
		rel = filepath.ToSlash(rel)
		for _, s := range d.Symbols {
			kw := keywordScore(qTokens, s.Content+" "+rel)
			sym := symbolScore(qTokens, s)
			score := kw
			switch opts.Mode {
			case ModeSymbol:
				score = sym
			case ModeHybrid:
				score = 0.35*kw + 0.65*sym
			}
			if score <= 0 {
				continue
			}
			out = append(out, Result{FilePath: rel, StartLine: s.StartLine, EndLine: s.EndLine, Score: score, Symbol: s, Content: s.Content, Context: stitchContext(d, s.StartLine, s.EndLine, opts.ContextLines)})
		}
		if len(d.Symbols) == 0 {
			kw := keywordScore(qTokens, d.Content+" "+rel)
			if kw > 0 {
				out = append(out, Result{FilePath: rel, Score: kw, Content: d.Content, Context: stitchDocumentContext(d, qTokens, opts.ContextLines)})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Score == out[b].Score {
			return out[a].FilePath < out[b].FilePath
		}
		return out[a].Score > out[b].Score
	})
	if len(out) > opts.TopK {
		out = out[:opts.TopK]
	}
	return out
}

func parseGoSymbols(path, content, root string) []*Symbol {
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, path, content, parser.ParseComments)
	if err != nil {
		return parseGenericSymbols(path, content, root)
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := strings.Split(content, "\n")
	var out []*Symbol
	add := func(name, kind, parent string, node ast.Node) {
		start, end := fs.Position(node.Pos()).Line, fs.Position(node.End()).Line
		if start < 1 || end > len(lines) {
			return
		}
		sig := strings.TrimSpace(lines[start-1])
		body := strings.Join(lines[start-1:end], "\n")
		out = append(out, &Symbol{Name: name, Kind: kind, Parent: parent, FilePath: rel, StartLine: start, EndLine: end, Signature: sig, Content: body})
	}
	for _, decl := range file.Decls {
		switch n := decl.(type) {
		case *ast.FuncDecl:
			parent := ""
			if n.Recv != nil && len(n.Recv.List) > 0 {
				parent = exprName(n.Recv.List[0].Type)
			}
			add(n.Name.Name, "method", parent, n)
		case *ast.GenDecl:
			for _, spec := range n.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					add(ts.Name.Name, "type", "", ts)
				}
			}
		}
	}
	return out
}

var genericDecl = regexp.MustCompile(`(?m)^\s*(?:async\s+)?(?:func|function|def|class|interface|struct|type)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func parseGenericSymbols(path, content, root string) []*Symbol {
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	lines := strings.Split(content, "\n")
	var out []*Symbol
	for _, m := range genericDecl.FindAllStringSubmatchIndex(content, -1) {
		name := content[m[2]:m[3]]
		line := 1 + strings.Count(content[:m[0]], "\n")
		end := line
		for j := line; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				break
			}
			end = j + 1
		}
		kind := strings.Fields(strings.TrimSpace(content[m[0]:m[1]]))[0]
		out = append(out, &Symbol{Name: name, Kind: kind, FilePath: rel, StartLine: line, EndLine: end, Signature: strings.TrimSpace(lines[line-1]), Content: strings.Join(lines[line-1:end], "\n")})
	}
	return out
}

func stitchContext(d *Document, start, end, n int) string {
	if n <= 0 {
		n = 3
	}
	lo, hi := start-n-1, end+n
	if lo < 0 {
		lo = 0
	}
	if hi > len(d.Lines) {
		hi = len(d.Lines)
	}
	// Include the package/import preamble, which makes isolated symbol snippets useful to an agent.
	pre := 0
	for pre < len(d.Lines) && pre < 20 {
		t := strings.TrimSpace(d.Lines[pre])
		pre++
		if pre > 1 && (strings.HasPrefix(t, "func ") || strings.HasPrefix(t, "type ")) {
			break
		}
	}
	if pre > lo {
		lo = 0
	}
	return strings.Join(d.Lines[lo:hi], "\n")
}

func stitchDocumentContext(d *Document, q []string, n int) string {
	for j, line := range d.Lines {
		if keywordScore(q, line) > 0 {
			return stitchContext(d, j+1, j+1, n)
		}
	}
	return d.Content
}

func keywordScore(q []string, text string) float64 {
	if len(q) == 0 {
		return 0
	}
	toks := tokenize(text)
	counts := map[string]int{}
	for _, t := range toks {
		counts[t]++
	}
	score := 0.0
	for _, term := range q {
		if counts[term] > 0 {
			score += 1 + 0.15*float64(counts[term]-1)
		}
	}
	return score / float64(len(q))
}
func symbolScore(q []string, s *Symbol) float64 {
	name := strings.ToLower(s.Name)
	score := 0.0
	for _, t := range q {
		if t == name {
			score += 3
		}
		if strings.Contains(name, t) {
			score += 1
		}
	}
	if strings.EqualFold(strings.Join(q, ""), s.Name) {
		score += 5
	}
	return score
}
func tokenize(s string) []string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			b.WriteByte(' ')
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Fields(b.String())
}
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return exprName(x.X)
	case *ast.IndexExpr:
		return exprName(x.X)
	}
	return ""
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func skipDirectory(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".mewcode", "dist", "build", "target", ".venv", "venv":
		return true
	}
	return strings.HasPrefix(name, ".") && name != "."
}
func skipFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".zip", ".gz", ".exe", ".dll", ".so", ".db", ".sqlite", ".lock":
		return true
	}
	return strings.HasPrefix(name, ".") && name != ".env.example"
}
func bytesBinary(data []byte) bool {
	n := len(data)
	if n > 4096 {
		n = 4096
	}
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

type EvalQuery struct {
	Text            string
	ExpectedSymbols []string
}
type ModeMetrics struct {
	Queries, Top5Hits          int
	Top5Accuracy, AvgLatencyMs float64
}
type EvalReport struct {
	Queries                 int
	Keyword, Symbol, Hybrid ModeMetrics
}

func DefaultEvalQueries(symbols []*Symbol) []EvalQuery {
	var out []EvalQuery
	variants := []string{"%s", "find %s", "where is %s", "implementation of %s", "call %s", "test %s"}
	for _, s := range symbols {
		for _, v := range variants {
			out = append(out, EvalQuery{Text: fmt.Sprintf(v, s.Name), ExpectedSymbols: []string{s.Name}})
		}
	}
	for len(out) < 30 && len(symbols) > 0 {
		s := symbols[len(out)%len(symbols)]
		out = append(out, EvalQuery{Text: s.Name, ExpectedSymbols: []string{s.Name}})
	}
	if len(out) == 0 {
		for _, text := range []string{
			"find authentication handler", "locate database repository", "where is configuration loaded",
			"find HTTP request client", "locate error recovery", "find command execution", "search token budget",
			"where is context compaction", "find session persistence", "locate file history", "find permission checker",
			"search tool registry", "where is agent loop", "find MCP server lifecycle", "locate task manager",
			"find embedding store", "search reranker", "locate prompt builder", "find memory extraction",
			"where is plan mode", "find verification gate", "search streaming executor", "locate config loader",
			"find shell command", "search websocket server", "locate TUI session", "find context window",
			"search code chunker", "where is model provider", "find retry strategy",
		} {
			out = append(out, EvalQuery{Text: text})
		}
	}
	return out
}

func Evaluate(idx *Index, queries []EvalQuery, topK int) EvalReport {
	report := EvalReport{Queries: len(queries)}
	report.Keyword = evalMode(idx, queries, topK, ModeKeyword)
	report.Symbol = evalMode(idx, queries, topK, ModeSymbol)
	report.Hybrid = evalMode(idx, queries, topK, ModeHybrid)
	return report
}
func evalMode(idx *Index, qs []EvalQuery, topK int, mode SearchMode) ModeMetrics {
	m := ModeMetrics{Queries: len(qs)}
	start := time.Now()
	for _, q := range qs {
		rs := idx.Search(q.Text, SearchOptions{Mode: mode, TopK: topK})
		hit := false
		for _, r := range rs {
			for _, e := range q.ExpectedSymbols {
				if r.Symbol != nil && strings.EqualFold(r.Symbol.Name, e) {
					hit = true
				}
			}
		}
		if hit {
			m.Top5Hits++
		}
	}
	if len(qs) > 0 {
		m.Top5Accuracy = float64(m.Top5Hits) / float64(len(qs)) * 100
	}
	m.AvgLatencyMs = float64(time.Since(start).Microseconds()) / 1000 / float64(max(1, len(qs)))
	return m
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
