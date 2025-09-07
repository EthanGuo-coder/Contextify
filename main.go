package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"
)

const version = "1.0.0-contextify"

// Config holds extraction configuration.
type Config struct {
	Path          string   `json:"path" yaml:"path"`
	Output        string   `json:"output" yaml:"output"`
	Format        string   `json:"format" yaml:"format"`
	Exclude       []string `json:"exclude" yaml:"exclude"`
	Include       []string `json:"include" yaml:"include"`
	StripComments bool     `json:"strip_comments" yaml:"strip_comments"`
	MaxTokens     int      `json:"max_tokens" yaml:"max_tokens"`
	AST           bool     `json:"ast" yaml:"ast"`
	Focus         string   `json:"focus" yaml:"focus"`
	Depth         int      `json:"depth" yaml:"depth"`
	Workers       int      `json:"workers" yaml:"workers"`
}

// FileInfo represents file extraction result
type FileInfo struct {
	Path     string   `json:"path" yaml:"path"`
	Language string   `json:"language" yaml:"language"`
	Content  string   `json:"content" yaml:"content"`
	Size     int64    `json:"size" yaml:"size"`
	AST      *ASTInfo `json:"ast,omitempty" yaml:"ast,omitempty"`
	// relevance weight for trimming (higher -> keep)
	Weight int `json:"-" yaml:"-"`
}

// ASTInfo holds lightweight AST summary for Go files
type ASTInfo struct {
	Package   string   `json:"package" yaml:"package"`
	Imports   []string `json:"imports" yaml:"imports"`
	Structs   []string `json:"structs" yaml:"structs"`
	Functions []string `json:"functions" yaml:"functions"`
}

// Context represents full project context
type Context struct {
	ProjectPath     string     `json:"project_path" yaml:"project_path"`
	TreeStructure   string     `json:"tree_structure" yaml:"tree_structure"`
	Files           []FileInfo `json:"files" yaml:"files"`
	TotalFiles      int        `json:"total_files" yaml:"total_files"`
	TotalSize       int64      `json:"total_size" yaml:"total_size"`
	EstimatedTokens int        `json:"estimated_tokens" yaml:"estimated_tokens"`
	Truncated       bool       `json:"truncated,omitempty" yaml:"truncated,omitempty"`
}

var defaultIgnorePatterns = []string{
	".git", ".svn", ".hg",
	"node_modules", "vendor", "target",
	"build", "dist", "out",
	"__pycache__", ".pytest_cache",
	"*.pyc", "*.pyo", "*.pyd",
	".DS_Store", "Thumbs.db",
	"*.log", "*.tmp", "*.temp",
	".idea", ".vscode", ".vs",
	"*.exe", "*.dll", "*.so", "*.dylib",
	"*.class", "*.jar",
	"coverage", ".nyc_output",
}

var languageMap = map[string]string{
	".go":    "go",
	".java":  "java",
	".py":    "python",
	".js":    "javascript",
	".ts":    "typescript",
	".rs":    "rust",
	".c":     "c",
	".cpp":   "cpp",
	".h":     "c",
	".hpp":   "cpp",
	".cs":    "csharp",
	".rb":    "ruby",
	".php":   "php",
	".swift": "swift",
	".kt":    "kotlin",
	".scala": "scala",
	".r":     "r",
	".m":     "matlab",
	".sh":    "shell",
	".bash":  "bash",
	".zsh":   "zsh",
	".ps1":   "powershell",
	".yaml":  "yaml",
	".yml":   "yaml",
	".json":  "json",
	".xml":   "xml",
	".html":  "html",
	".css":   "css",
	".scss":  "scss",
	".sass":  "sass",
	".less":  "less",
	".sql":   "sql",
	".md":    "markdown",
	".rst":   "restructuredtext",
	".tex":   "latex",
}

var rootCmd = &cobra.Command{
	Use:     "contextify",
	Short:   "Contextify — AI Code Context Extractor",
	Long:    `Contextify extracts project code context optimized for AI prompts (AST, structure, focused slices).`,
	Version: version,
}

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract code context from a project",
	RunE:  runExtract,
}

var (
	cfgPath          string
	cfgOutput        string
	cfgFormat        string
	cfgExclude       []string
	cfgInclude       []string
	cfgStripComments bool
	cfgMaxTokens     int
	cfgAST           bool
	cfgFocus         string
	cfgDepth         int
	cfgWorkers       int
)

func init() {
	extractCmd.Flags().StringVarP(&cfgPath, "path", "p", ".", "Path to the project directory")
	extractCmd.Flags().StringVarP(&cfgOutput, "output", "o", "", "Output file path (default: stdout)")
	extractCmd.Flags().StringVarP(&cfgFormat, "format", "f", "markdown", "Output format (markdown, json, yaml)")
	extractCmd.Flags().StringSliceVarP(&cfgExclude, "exclude", "e", []string{}, "Patterns to exclude (glob)")
	extractCmd.Flags().StringSliceVarP(&cfgInclude, "include", "i", []string{}, "Patterns to include (glob)")
	extractCmd.Flags().BoolVar(&cfgStripComments, "strip-comments", false, "Strip comments from code")
	extractCmd.Flags().IntVar(&cfgMaxTokens, "max-tokens", 0, "Maximum tokens (0 for unlimited)")
	extractCmd.Flags().BoolVar(&cfgAST, "ast", false, "Enable AST extraction for Go files")
	extractCmd.Flags().StringVar(&cfgFocus, "focus", "", "Focus symbol (e.g. FuncName or Type.Method) for definition tracing")
	extractCmd.Flags().IntVar(&cfgDepth, "depth", 1, "Depth for focus tracing (default 1)")
	extractCmd.Flags().IntVar(&cfgWorkers, "workers", 4, "Number of concurrent workers for file processing")

	rootCmd.AddCommand(extractCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runExtract(cmd *cobra.Command, args []string) error {
	cfg := &Config{
		Path:          cfgPath,
		Output:        cfgOutput,
		Format:        cfgFormat,
		Exclude:       append([]string{}, defaultIgnorePatterns...),
		Include:       cfgInclude,
		StripComments: cfgStripComments,
		MaxTokens:     cfgMaxTokens,
		AST:           cfgAST,
		Focus:         cfgFocus,
		Depth:         cfgDepth,
		Workers:       cfgWorkers,
	}

	// merge CLI exclude customizations after defaults
	if len(cfgExclude) > 0 {
		cfg.Exclude = append(cfg.Exclude, cfgExclude...)
	}

	// Load project-level config if present
	if configFile := filepath.Join(cfg.Path, ".ai-context.yaml"); fileExists(configFile) {
		if err := loadConfigFile(configFile, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load config file: %v\n", err)
		}
	}

	// Validate
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Depth < 0 {
		cfg.Depth = 1
	}

	ctx, err := extractContext(cfg)
	if err != nil {
		return fmt.Errorf("failed to extract context: %w", err)
	}

	outStr, err := generateOutput(ctx, cfg.Format)
	if err != nil {
		return fmt.Errorf("failed to generate output: %w", err)
	}

	if cfg.Output == "" {
		fmt.Print(outStr)
	} else {
		if err := os.WriteFile(cfg.Output, []byte(outStr), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Printf("Context extracted successfully to %s\n", cfg.Output)
	}

	if cfg.MaxTokens > 0 && ctx.EstimatedTokens > cfg.MaxTokens {
		fmt.Fprintf(os.Stderr, "Warning: Estimated tokens (%d) exceed maximum (%d)\n", ctx.EstimatedTokens, cfg.MaxTokens)
	}

	return nil
}

func extractContext(cfg *Config) (*Context, error) {
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, err
	}

	ctx := &Context{
		ProjectPath: absPath,
		Files:       []FileInfo{},
	}

	// Merge .gitignore patterns
	gitignore := readGitignore(cfg.Path)
	if len(gitignore) > 0 {
		cfg.Exclude = append(cfg.Exclude, gitignore...)
	}

	// Walk project to build file list and tree
	var treeBuf bytes.Buffer
	files := []string{}

	err = filepath.Walk(cfg.Path, func(path string, info os.FileInfo, wErr error) error {
		if wErr != nil {
			// continue on walk error but log
			fmt.Fprintf(os.Stderr, "Warning: walk error for %s: %v\n", path, wErr)
			return nil
		}
		relPath, _ := filepath.Rel(cfg.Path, path)
		if relPath == "." {
			return nil
		}

		if shouldExclude(relPath, cfg.Exclude, cfg.Include) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		depth := strings.Count(relPath, string(os.PathSeparator))
		indent := strings.Repeat("  ", depth)
		name := filepath.Base(relPath)
		if info.IsDir() {
			fmt.Fprintf(&treeBuf, "%s%s/\n", indent, name)
		} else {
			fmt.Fprintf(&treeBuf, "%s%s\n", indent, name)
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	ctx.TreeStructure = treeBuf.String()

	// Concurrent processing of files
	fileCh := make(chan string, len(files))
	resultCh := make(chan *FileInfo, len(files))
	var wg sync.WaitGroup

	workers := cfg.Workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range fileCh {
				fi, err := processFile(path, cfg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to process %s: %v\n", path, err)
					continue
				}
				resultCh <- fi
			}
		}()
	}

	for _, f := range files {
		fileCh <- f
	}
	close(fileCh)

	// wait and then close resultCh
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for fi := range resultCh {
		ctx.Files = append(ctx.Files, *fi)
		ctx.TotalSize += fi.Size
	}

	ctx.TotalFiles = len(ctx.Files)

	// If AST/focus requested and language is go, perform Go-level analysis (call graph + focus tracing)
	if cfg.AST || cfg.Focus != "" {
		performGoAnalysis(ctx, cfg)
	}

	ctx.EstimatedTokens = estimateTokens(ctx)

	// If max tokens set and exceeded: try to trim files with a small heuristic
	if cfg.MaxTokens > 0 && ctx.EstimatedTokens > cfg.MaxTokens {
		trimmed, truncated := trimFilesToTokenLimit(ctx, cfg.MaxTokens)
		ctx.Files = trimmed
		ctx.TotalFiles = len(trimmed)
		var totalSize int64
		for _, f := range trimmed {
			totalSize += f.Size
		}
		ctx.TotalSize = totalSize
		ctx.EstimatedTokens = estimateTokens(ctx)
		ctx.Truncated = truncated
	}

	return ctx, nil
}

func processFile(path string, cfg *Config) (*FileInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	relPath, _ := filepath.Rel(cfg.Path, path)
	ext := strings.ToLower(filepath.Ext(path))
	language := languageMap[ext]
	if language == "" {
		language = "plaintext"
	}
	contentStr := string(data)

	if cfg.StripComments {
		contentStr = stripComments(contentStr, language)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	fi := &FileInfo{
		Path:     relPath,
		Language: language,
		Content:  contentStr,
		Size:     info.Size(),
		Weight:   1,
	}

	// optionally parse AST for Go
	if cfg.AST && language == "go" {
		astInfo := parseGoASTFromBytes(data)
		fi.AST = astInfo
	}

	return fi, nil
}

// parseGoASTFromBytes returns a lightweight AST summary (no deep analysis)
func parseGoASTFromBytes(src []byte) *ASTInfo {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		// if parse fails, return nil to avoid breaking pipeline
		return nil
	}
	ai := &ASTInfo{}
	if f.Name != nil {
		ai.Package = f.Name.Name
	}
	for _, imp := range f.Imports {
		str := strings.Trim(imp.Path.Value, `"`)
		ai.Imports = append(ai.Imports, str)
	}
	// iterate top-level declarations
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// type declarations -> structs
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					switch ts.Type.(type) {
					case *ast.StructType:
						ai.Structs = append(ai.Structs, ts.Name.Name)
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) > 0 {
				// method; include receiver type as prefix
				recvType := exprString(d.Recv.List[0].Type)
				ai.Functions = append(ai.Functions, fmt.Sprintf("(%s).%s", recvType, d.Name.Name))
			} else {
				ai.Functions = append(ai.Functions, d.Name.Name)
			}
		}
	}
	return ai
}

func exprString(expr ast.Expr) string {
	var buf bytes.Buffer
	_ = formatNode(&buf, expr)
	return buf.String()
}

func formatNode(w io.Writer, n interface{}) error {
	// a tiny helper to print simple expressions; we avoid importing go/printer to keep it minimal
	// if needed, switch to go/printer for full fidelity
	switch v := n.(type) {
	case *ast.Ident:
		_, _ = io.WriteString(w, v.Name)
	case *ast.StarExpr:
		_, _ = io.WriteString(w, "*")
		_ = formatNode(w, v.X)
	case *ast.SelectorExpr:
		_ = formatNode(w, v.X)
		_, _ = io.WriteString(w, ".")
		_ = formatNode(w, v.Sel)
	default:
		// fallback to empty
	}
	return nil
}

// performGoAnalysis builds a simple call graph for functions and applies focus/depth tracing
func performGoAnalysis(ctx *Context, cfg *Config) {
	// Build maps: filename -> source bytes, funcName -> (file, start, end)
	type funcLoc struct {
		File   string
		Name   string
		Start  int
		End    int
		Weight int
	}
	funcs := map[string]*funcLoc{}
	fileSrc := map[string][]byte{}

	// Parse all go files to build call graph
	fset := token.NewFileSet()
	fileASTs := map[string]*ast.File{}
	for i := range ctx.Files {
		f := &ctx.Files[i]
		if f.Language != "go" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(ctx.ProjectPath, f.Path))
		if err != nil {
			continue
		}
		fileSrc[f.Path] = raw
		astFile, err := parser.ParseFile(fset, f.Path, raw, parser.ParseComments)
		if err != nil {
			continue
		}
		fileASTs[f.Path] = astFile

		// find function declarations
		for _, decl := range astFile.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name != nil {
				name := fd.Name.Name
				start := fset.Position(fd.Pos()).Offset
				end := fset.Position(fd.End()).Offset
				key := name
				// include method receiver if present
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					recv := exprString(fd.Recv.List[0].Type)
					key = fmt.Sprintf("%s.%s", recv, name)
				}
				funcs[key] = &funcLoc{
					File:   f.Path,
					Name:   key,
					Start:  start,
					End:    end,
					Weight: 10, // default higher weight
				}
			}
		}
	}

	// Build call graph: function -> called function names
	callGraph := map[string]map[string]struct{}{}
	for filePath, astFile := range fileASTs {
		ast.Inspect(astFile, func(n ast.Node) bool {
			// We look for CallExpr; extract simple Ident or SelectorExpr names
			if call, ok := n.(*ast.CallExpr); ok {
				var callee string
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					callee = fun.Name
				case *ast.SelectorExpr:
					// could be pkg.Func or expr.Method
					if id, ok := fun.X.(*ast.Ident); ok {
						callee = fmt.Sprintf("%s.%s", id.Name, fun.Sel.Name)
					} else {
						// fallback: Type.Method or something
						callee = fun.Sel.Name
					}
				}
				// find enclosing function for this call
				parent := findEnclosingFunc(astFile, call.Pos())
				if parent != nil && parent.Name != nil {
					parentName := parent.Name.Name
					// method receiver?
					if parent.Recv != nil && len(parent.Recv.List) > 0 {
						r := exprString(parent.Recv.List[0].Type)
						parentName = fmt.Sprintf("%s.%s", r, parentName)
					}
					if _, ok := callGraph[parentName]; !ok {
						callGraph[parentName] = map[string]struct{}{}
					}
					if callee != "" {
						callGraph[parentName][callee] = struct{}{}
					}
				}
			}
			return true
		})
		_ = filePath
	}

	// If focus specified, BFS on call graph to depth and mark relevant functions/files
	if cfg.Focus != "" {
		// normalize focus: try to match exactly or suffix match
		queue := []string{cfg.Focus}
		visited := map[string]struct{}{}
		depth := 0
		nextQueue := []string{}
		for depth <= cfg.Depth && len(queue) > 0 {
			for _, cur := range queue {
				// find function keys matching cur (exact or suffix)
				for k, fl := range funcs {
					if k == cur || strings.HasSuffix(k, cur) || strings.HasSuffix(k, "."+cur) {
						visited[k] = struct{}{}
						// mark file's weight high
						for i := range ctx.Files {
							if ctx.Files[i].Path == fl.File {
								ctx.Files[i].Weight += 1000
							}
						}
						// enqueue callees
						if callees, ok := callGraph[k]; ok {
							for callee := range callees {
								nextQueue = append(nextQueue, callee)
							}
						}
					}
				}
			}
			queue = nextQueue
			nextQueue = []string{}
			depth++
		}
		// Additionally, include callers of focus (usage tracing) by scanning callGraph
		for caller, callees := range callGraph {
			for callee := range callees {
				if _, ok := visited[callee]; ok {
					if callerFL, ok := funcs[caller]; ok {
						for i := range ctx.Files {
							if ctx.Files[i].Path == callerFL.File {
								ctx.Files[i].Weight += 500
							}
						}
					}
				}
			}
		}
	}

	// Attach AST summary to ctx.Files if AST requested (we may have parsed earlier)
	// This is already done in processFile when cfg.AST true.
}

// findEnclosingFunc finds ast.FuncDecl that encloses the given position
func findEnclosingFunc(file *ast.File, pos token.Pos) *ast.FuncDecl {
	var found *ast.FuncDecl
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok {
			if pos >= fd.Pos() && pos <= fd.End() {
				found = fd
				break
			}
		}
	}
	return found
}

func stripComments(content string, language string) string {
	// Use robust multiline regexes with flags
	switch language {
	case "go", "java", "javascript", "typescript", "c", "cpp", "csharp", "rust", "swift", "kotlin", "scala":
		// single-line // and multi-line /* */
		reSingle := regexp.MustCompile(`(?m)//.*$`)
		content = reSingle.ReplaceAllString(content, "")
		reMulti := regexp.MustCompile(`(?s)/\*.*?\*/`)
		content = reMulti.ReplaceAllString(content, "")
	case "python", "ruby", "shell", "bash", "zsh", "powershell", "yaml", "r":
		reHash := regexp.MustCompile(`(?m)#.*$`)
		content = reHash.ReplaceAllString(content, "")
	case "html", "xml":
		re := regexp.MustCompile(`(?s)<!--.*?-->`)
		content = re.ReplaceAllString(content, "")
	case "css", "scss", "sass", "less":
		re := regexp.MustCompile(`(?s)/\*.*?\*/`)
		content = re.ReplaceAllString(content, "")
	case "sql":
		reLine := regexp.MustCompile(`(?m)--.*$`)
		content = reLine.ReplaceAllString(content, "")
		reMulti := regexp.MustCompile(`(?s)/\*.*?\*/`)
		content = reMulti.ReplaceAllString(content, "")
	}

	// Remove consecutive empty lines and trim each line
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " \t")
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return strings.Join(out, "\n")
}

func shouldExclude(path string, excludePatterns []string, includePatterns []string) bool {
	// If include patterns specified: only include those matching
	if len(includePatterns) > 0 {
		included := false
		for _, pat := range includePatterns {
			match, _ := doublestar.Match(pat, path)
			if match {
				included = true
				break
			}
			// also try matching basename
			base := filepath.Base(path)
			m2, _ := doublestar.Match(pat, base)
			if m2 {
				included = true
				break
			}
			if strings.Contains(path, pat) {
				included = true
				break
			}
		}
		if !included {
			return true
		}
	}

	negations := []string{}
	for _, pat := range excludePatterns {
		if strings.HasPrefix(pat, "!") {
			negations = append(negations, strings.TrimPrefix(pat, "!"))
		}
	}

	// Check exclude patterns
	for _, pat := range excludePatterns {
		if pat == "" {
			continue
		}
		if strings.HasPrefix(pat, "!") {
			// handled later as negation
			continue
		}
		// try absolute pattern match against path and basename
		if ok, _ := doublestar.Match(pat, path); ok {
			return true
		}
		if ok, _ := doublestar.Match(pat, filepath.Base(path)); ok {
			return true
		}
		// fallback to substring
		if strings.Contains(path, pat) {
			return true
		}
	}

	// Apply negations: if any negation matches, do NOT exclude
	for _, n := range negations {
		if ok, _ := doublestar.Match(n, path); ok {
			return false
		}
		if strings.Contains(path, n) {
			return false
		}
	}

	return false
}

func readGitignore(projectPath string) []string {
	gitignorePath := filepath.Join(projectPath, ".gitignore")
	if !fileExists(gitignorePath) {
		return nil
	}
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	res := []string{}
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		// accept patterns and keep '!' negations
		res = append(res, ln)
	}
	return res
}

func estimateTokens(ctx *Context) int {
	totalChars := len(ctx.TreeStructure)
	for _, f := range ctx.Files {
		totalChars += len(f.Path) + len(f.Content)
		if f.AST != nil {
			// small safe addition for AST meta
			totalChars += len(strings.Join(f.AST.Functions, ",")) + len(strings.Join(f.AST.Structs, ","))
		}
	}
	// heuristic: 1 token ~= 4 chars
	return totalChars / 4
}

func generateOutput(ctx *Context, format string) (string, error) {
	switch strings.ToLower(format) {
	case "json":
		return generateJSON(ctx)
	case "yaml", "yml":
		return generateYAML(ctx)
	case "markdown", "md":
		return generateMarkdown(ctx)
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

func generateJSON(ctx *Context) (string, error) {
	b, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func generateYAML(ctx *Context) (string, error) {
	b, err := yaml.Marshal(ctx)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func generateMarkdown(ctx *Context) (string, error) {
	var b strings.Builder
	b.WriteString("# Project Context (Contextify)\n\n")
	b.WriteString(fmt.Sprintf("**Project Path:** `%s`\n\n", ctx.ProjectPath))
	b.WriteString(fmt.Sprintf("**Total Files:** %d\n\n", ctx.TotalFiles))
	b.WriteString(fmt.Sprintf("**Total Size:** %d bytes\n\n", ctx.TotalSize))
	b.WriteString(fmt.Sprintf("**Estimated Tokens:** %d\n\n", ctx.EstimatedTokens))
	if ctx.Truncated {
		b.WriteString("> **Note:** context was truncated to satisfy token limits.\n\n")
	}
	b.WriteString("## Directory Structure\n\n")
	b.WriteString("```\n")
	b.WriteString(ctx.TreeStructure)
	b.WriteString("```\n\n")

	// group by language
	filesByLang := map[string][]FileInfo{}
	for _, f := range ctx.Files {
		filesByLang[f.Language] = append(filesByLang[f.Language], f)
	}
	langs := make([]string, 0, len(filesByLang))
	for k := range filesByLang {
		langs = append(langs, k)
	}
	sort.Strings(langs)

	for _, lang := range langs {
		files := filesByLang[lang]
		b.WriteString(fmt.Sprintf("### %s Files\n\n", strings.Title(lang)))
		// sort by path
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		for _, f := range files {
			b.WriteString(fmt.Sprintf("#### `%s` — %d bytes\n\n", f.Path, f.Size))
			if f.AST != nil {
				b.WriteString("**AST Summary:**\n\n")
				if f.AST.Package != "" {
					b.WriteString(fmt.Sprintf("- Package: `%s`\n", f.AST.Package))
				}
				if len(f.AST.Imports) > 0 {
					b.WriteString(fmt.Sprintf("- Imports: `%s`\n", strings.Join(f.AST.Imports, ", ")))
				}
				if len(f.AST.Structs) > 0 {
					b.WriteString(fmt.Sprintf("- Structs: `%s`\n", strings.Join(f.AST.Structs, ", ")))
				}
				if len(f.AST.Functions) > 0 {
					b.WriteString(fmt.Sprintf("- Functions: `%s`\n", strings.Join(f.AST.Functions, ", ")))
				}
				b.WriteString("\n")
			}

			blockLang := lang
			if blockLang == "plaintext" {
				blockLang = ""
			}
			b.WriteString(fmt.Sprintf("```%s\n", blockLang))
			b.WriteString(f.Content)
			if !strings.HasSuffix(f.Content, "\n") {
				b.WriteString("\n")
			}
			b.WriteString("```\n\n")
		}
	}

	// footer
	b.WriteString(fmt.Sprintf("_Generated by Contextify on %s_\n", time.Now().UTC().Format(time.RFC3339)))
	return b.String(), nil
}

func loadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return err
	}
	// Merge with precedence: CLI already applied; only apply missing fields
	if cfg.Format == "" && fileCfg.Format != "" {
		cfg.Format = fileCfg.Format
	}
	if len(fileCfg.Exclude) > 0 {
		cfg.Exclude = append(cfg.Exclude, fileCfg.Exclude...)
	}
	if len(fileCfg.Include) > 0 && len(cfg.Include) == 0 {
		cfg.Include = fileCfg.Include
	}
	if !cfg.StripComments && fileCfg.StripComments {
		cfg.StripComments = fileCfg.StripComments
	}
	if cfg.MaxTokens == 0 && fileCfg.MaxTokens > 0 {
		cfg.MaxTokens = fileCfg.MaxTokens
	}
	if !cfg.AST && fileCfg.AST {
		cfg.AST = fileCfg.AST
	}
	if cfg.Focus == "" && fileCfg.Focus != "" {
		cfg.Focus = fileCfg.Focus
	}
	if cfg.Depth == 0 && fileCfg.Depth > 0 {
		cfg.Depth = fileCfg.Depth
	}
	if cfg.Workers == 0 && fileCfg.Workers > 0 {
		cfg.Workers = fileCfg.Workers
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// trimFilesToTokenLimit trims ctx.Files to try to fit under tokenLimit. Returns trimmed slice and whether truncation happened.
func trimFilesToTokenLimit(ctx *Context, tokenLimit int) ([]FileInfo, bool) {
	// Heuristic:
	// 1) Sort files by (weight desc, size asc) so we keep relevant small files.
	// 2) Add until estimate <= tokenLimit.
	files := make([]FileInfo, len(ctx.Files))
	copy(files, ctx.Files)
	sort.Slice(files, func(i, j int) bool {
		// higher weight first; then smaller size first
		if files[i].Weight != files[j].Weight {
			return files[i].Weight > files[j].Weight
		}
		return files[i].Size < files[j].Size
	})

	acc := 0
	out := []FileInfo{}
	for _, f := range files {
		// rough tokens for this file
		toks := (len(f.Path) + len(f.Content)) / 4
		if acc+toks > tokenLimit {
			// skip file
			continue
		}
		out = append(out, f)
		acc += toks
	}
	truncated := len(out) < len(ctx.Files)
	return out, truncated
}
