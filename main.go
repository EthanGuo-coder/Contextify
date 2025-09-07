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

// Config holds extraction configuration read from flags or .ai-context.yaml.
// Fields map to CLI flags and to the YAML config file.
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

// FileInfo represents the extracted metadata and (optionally) content for one file.
type FileInfo struct {
	Path     string   `json:"path" yaml:"path"`
	Language string   `json:"language" yaml:"language"`
	Content  string   `json:"content" yaml:"content"`
	Size     int64    `json:"size" yaml:"size"`
	AST      *ASTInfo `json:"ast,omitempty" yaml:"ast,omitempty"`
	Weight   int      `json:"-" yaml:"-"`
}

// ASTInfo is a lightweight summary of a Go file's top-level AST details.
type ASTInfo struct {
	Package   string   `json:"package" yaml:"package"`
	Imports   []string `json:"imports" yaml:"imports"`
	Structs   []string `json:"structs" yaml:"structs"`
	Functions []string `json:"functions" yaml:"functions"`
}

// Context is the full project extraction result to be serialized.
type Context struct {
	ProjectPath     string     `json:"project_path" yaml:"project_path"`
	TreeStructure   string     `json:"tree_structure" yaml:"tree_structure"`
	Files           []FileInfo `json:"files" yaml:"files"`
	TotalFiles      int        `json:"total_files" yaml:"total_files"`
	TotalSize       int64      `json:"total_size" yaml:"total_size"`
	EstimatedTokens int        `json:"estimated_tokens" yaml:"estimated_tokens"`
	Truncated       bool       `json:"truncated,omitempty" yaml:"truncated,omitempty"`
}

// defaultIgnorePatterns are common directory/file patterns that should be skipped.
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

// languageMap maps file extensions to a short language identifier.
// Used to group output and to apply language-specific logic (e.g. comment stripping).
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
	// CLI flags with sensible defaults.
	extractCmd.Flags().StringVarP(&cfgPath, "path", "p", ".", "Path to the project directory")
	extractCmd.Flags().StringVarP(&cfgOutput, "output", "o", "", "Output file path (default: auto-generated in project dir)")
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

// runExtract composes the configuration, reads optional .ai-context.yaml, and runs extraction.
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

	// Merge user-specified exclude patterns after defaults.
	if len(cfgExclude) > 0 {
		cfg.Exclude = append(cfg.Exclude, cfgExclude...)
	}

	// Load project-level config if present.
	if configFile := filepath.Join(cfg.Path, ".ai-context.yaml"); fileExists(configFile) {
		if err := loadConfigFile(configFile, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load config file: %v\n", err)
		}
	}

	// Add current executable name patterns to exclude to avoid self-inclusion.
	if exePath, err := os.Executable(); err == nil {
		exeBase := filepath.Base(exePath)                              // e.g. "contextify" or "contextify.exe"
		exeNoExt := strings.TrimSuffix(exeBase, filepath.Ext(exeBase)) // e.g. "contextify"

		// Append a few likely variants to the exclude list.
		cfg.Exclude = appendUnique(cfg.Exclude, exeBase)
		if exeNoExt != exeBase {
			cfg.Exclude = appendUnique(cfg.Exclude, exeNoExt)
		}
		cfg.Exclude = appendUnique(cfg.Exclude, fmt.Sprintf("%s-*.md", exeNoExt))
		cfg.Exclude = appendUnique(cfg.Exclude, fmt.Sprintf("%s_*.md", exeNoExt))
		cfg.Exclude = appendUnique(cfg.Exclude, fmt.Sprintf("%s-*.json", exeNoExt))
		cfg.Exclude = appendUnique(cfg.Exclude, fmt.Sprintf("%s-*.yaml", exeNoExt))
		cfg.Exclude = appendUnique(cfg.Exclude, fmt.Sprintf("%s-*.yml", exeNoExt))
		cfg.Exclude = appendUnique(cfg.Exclude, fmt.Sprintf("%s.md", exeNoExt))
	}

	// Validate numeric options.
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.Depth < 0 {
		cfg.Depth = 1
	}

	// Perform extraction.
	ctx, err := extractContext(cfg)
	if err != nil {
		return fmt.Errorf("failed to extract context: %w", err)
	}

	outStr, err := generateOutput(ctx, cfg.Format)
	if err != nil {
		return fmt.Errorf("failed to generate output: %w", err)
	}

	// Determine output destination if not provided.
	if cfg.Output == "" {
		ext := "md"
		switch strings.ToLower(cfg.Format) {
		case "json":
			ext = "json"
		case "yaml", "yml":
			ext = "yaml"
		case "markdown", "md":
			ext = "md"
		}
		tstamp := time.Now().UTC().Format("20060102_150405")
		defaultName := fmt.Sprintf("%s-%s.%s", filepath.Base(strings.TrimSuffix(os.Args[0], filepath.Ext(os.Args[0]))), tstamp, ext)
		outPath := filepath.Join(cfg.Path, defaultName)
		if err := os.WriteFile(outPath, []byte(outStr), 0644); err != nil {
			// fallback to cwd
			cwd, _ := os.Getwd()
			outPath = filepath.Join(cwd, defaultName)
			if err2 := os.WriteFile(outPath, []byte(outStr), 0644); err2 != nil {
				// final fallback: stdout
				fmt.Fprintln(os.Stderr, "Warning: failed to write to project dir or cwd; printing to stdout")
				fmt.Print(outStr)
				return nil
			}
		}
		fmt.Printf("Context extracted successfully to %s\n", outPath)
	} else {
		// Write to user-specified output.
		if err := os.WriteFile(cfg.Output, []byte(outStr), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Printf("Context extracted successfully to %s\n", cfg.Output)
	}

	// Inform user if estimated tokens exceed configured maximum.
	if cfg.MaxTokens > 0 && ctx.EstimatedTokens > cfg.MaxTokens {
		fmt.Fprintf(os.Stderr, "Warning: Estimated tokens (%d) exceed maximum (%d)\n", ctx.EstimatedTokens, cfg.MaxTokens)
	}

	return nil
}

// appendUnique appends val to slice only if it's not already present.
func appendUnique(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// extractContext walks the project tree, filters files, and produces a Context.
func extractContext(cfg *Config) (*Context, error) {
	absPath, err := filepath.Abs(cfg.Path)
	if err != nil {
		return nil, err
	}

	ctx := &Context{
		ProjectPath: absPath,
		Files:       []FileInfo{},
	}

	// Add patterns from .gitignore if present.
	gitignore := readGitignore(cfg.Path)
	if len(gitignore) > 0 {
		cfg.Exclude = append(cfg.Exclude, gitignore...)
	}

	// Walk the filesystem to collect files and build a human-friendly tree string.
	var treeBuf bytes.Buffer
	files := []string{}

	err = filepath.Walk(cfg.Path, func(path string, info os.FileInfo, wErr error) error {
		if wErr != nil {
			// Non-fatal walk error; log and continue.
			fmt.Fprintf(os.Stderr, "Warning: walk error for %s: %v\n", path, wErr)
			return nil
		}
		relPath, _ := filepath.Rel(cfg.Path, path)
		if relPath == "." {
			return nil
		}

		// Determine whether to skip this path.
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

	// Concurrent processing of files using worker goroutines.
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

	// Close resultCh after workers finish.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for fi := range resultCh {
		ctx.Files = append(ctx.Files, *fi)
		ctx.TotalSize += fi.Size
	}

	ctx.TotalFiles = len(ctx.Files)

	// If AST extraction or focus tracing is requested, perform lightweight Go analysis.
	if cfg.AST || cfg.Focus != "" {
		performGoAnalysis(ctx, cfg)
	}

	ctx.EstimatedTokens = estimateTokens(ctx)

	// If the result exceeds token limit, trim files heuristically.
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

// processFile reads file bytes, decides language, strips comments (optional), and returns FileInfo.
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

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// If file looks binary, include a small placeholder rather than raw contents.
	if isBinary(data) {
		return &FileInfo{
			Path:     relPath,
			Language: "binary",
			Content:  fmt.Sprintf("<binary file omitted, %d bytes>", info.Size()),
			Size:     info.Size(),
			Weight:   0, // binaries are deprioritized
		}, nil
	}

	// Avoid embedding very large files to keep token usage reasonable.
	const maxContentBytes = 1 << 20 // 1 MB
	contentStr := string(data)
	if info.Size() > int64(maxContentBytes) {
		contentStr = fmt.Sprintf("<file too large, %d bytes, omitted>", info.Size())
	} else {
		if cfg.StripComments {
			contentStr = stripComments(contentStr, language)
		}
	}

	fi := &FileInfo{
		Path:     relPath,
		Language: language,
		Content:  contentStr,
		Size:     info.Size(),
		Weight:   1,
	}

	// Optionally parse a lightweight AST summary for Go files.
	if cfg.AST && language == "go" {
		astInfo := parseGoASTFromBytes([]byte(contentStr))
		fi.AST = astInfo
	}

	return fi, nil
}

// isBinary uses a few fast heuristics to determine whether data is binary.
// - checks for ELF/PE headers
// - NUL bytes in the first 512 bytes
// - proportion of non-printable characters in a sample
func isBinary(data []byte) bool {
	if len(data) >= 4 && data[0] == 0x7f && bytes.Equal(data[1:4], []byte("ELF")) {
		return true
	}
	if len(data) >= 2 && data[0] == 'M' && data[1] == 'Z' {
		return true
	}
	// If contains NUL, almost certainly binary.
	for i := 0; i < len(data) && i < 512; i++ {
		if data[i] == 0 {
			return true
		}
	}
	// Heuristic: proportion of control characters in a sample.
	sample := len(data)
	if sample > 1024 {
		sample = 1024
	}
	if sample == 0 {
		return false
	}
	nonText := 0
	for i := 0; i < sample; i++ {
		b := data[i]
		// allow common whitespace and UTF-8 continuation bytes (>=0x80)
		if b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if b < 0x20 {
			nonText++
		}
	}
	return (nonText*100)/sample > 10 // >10% control chars => binary
}

// parseGoASTFromBytes returns a compact AST summary for a Go source file.
// It intentionally keeps the result small and robust to parse errors.
func parseGoASTFromBytes(src []byte) *ASTInfo {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		// If parse fails, return nil — keep pipeline resilient.
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
	// Collect top-level structs and functions.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// Type declarations -> record struct names.
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
				// method: include receiver type as prefix
				recvType := exprString(d.Recv.List[0].Type)
				ai.Functions = append(ai.Functions, fmt.Sprintf("(%s).%s", recvType, d.Name.Name))
			} else {
				ai.Functions = append(ai.Functions, d.Name.Name)
			}
		}
	}
	return ai
}

// exprString renders a small subset of ast.Expr to a string.
// This helper is intentionally minimal — it avoids importing go/printer.
func exprString(expr ast.Expr) string {
	var buf bytes.Buffer
	_ = formatNode(&buf, expr)
	return buf.String()
}

// formatNode writes a small set of expression node types to w.
// It handles basic identifiers, pointers, and selector expressions.
func formatNode(w io.Writer, n interface{}) error {
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
		// unsupported node types are omitted for brevity.
	}
	return nil
}

// performGoAnalysis builds a simple call graph for Go files and marks files
// according to the configured focus symbol and depth. Marking influences
// which files are kept when trimming to token limits.
func performGoAnalysis(ctx *Context, cfg *Config) {
	// funcLoc holds function location metadata used to map functions to files.
	type funcLoc struct {
		File   string
		Name   string
		Start  int
		End    int
		Weight int
	}
	funcs := map[string]*funcLoc{}
	fileSrc := map[string][]byte{}

	// Parse all Go files and collect function positions.
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

		for _, decl := range astFile.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name != nil {
				name := fd.Name.Name
				start := fset.Position(fd.Pos()).Offset
				end := fset.Position(fd.End()).Offset
				key := name
				// include receiver type for methods to disambiguate
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					recv := exprString(fd.Recv.List[0].Type)
					key = fmt.Sprintf("%s.%s", recv, name)
				}
				funcs[key] = &funcLoc{
					File:   f.Path,
					Name:   key,
					Start:  start,
					End:    end,
					Weight: 10,
				}
			}
		}
	}

	// Build a simple call graph: caller -> callee set
	callGraph := map[string]map[string]struct{}{}
	for _, astFile := range fileASTs {
		ast.Inspect(astFile, func(n ast.Node) bool {
			// Look for CallExpr and extract a callee name in common forms.
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
						// fallback to method name only
						callee = fun.Sel.Name
					}
				}
				// Find enclosing function for this call and connect edges.
				parent := findEnclosingFunc(astFile, call.Pos())
				if parent != nil && parent.Name != nil {
					parentName := parent.Name.Name
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
	}

	// If a focus symbol is provided, perform a breadth-first search from it
	// and boost weights for visited functions/files to prioritize them.
	if cfg.Focus != "" {
		queue := []string{cfg.Focus}
		visited := map[string]struct{}{}
		depth := 0
		nextQueue := []string{}
		for depth <= cfg.Depth && len(queue) > 0 {
			for _, cur := range queue {
				// match function keys by exact or suffix match
				for k, fl := range funcs {
					if k == cur || strings.HasSuffix(k, cur) || strings.HasSuffix(k, "."+cur) {
						visited[k] = struct{}{}
						// mark file's weight high
						for i := range ctx.Files {
							if ctx.Files[i].Path == fl.File {
								ctx.Files[i].Weight += 1000
							}
						}
						// enqueue callees for next level
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
		// Also mark callers of the visited functions to preserve context.
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
}

// findEnclosingFunc returns the FuncDecl that contains pos, if any.
// This is a linear scan over top-level decls which is sufficient for small files.
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

// stripComments removes comments for common languages using regex heuristics.
// It is conservative: it removes single-line and multi-line comment forms,
// then collapses empty lines to produce a denser output.
func stripComments(content string, language string) string {
	// Use robust regexes per language family.
	switch language {
	case "go", "java", "javascript", "typescript", "c", "cpp", "csharp", "rust", "swift", "kotlin", "scala":
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

	// Trim trailing whitespace and remove empty lines to keep output compact.
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

// shouldExclude returns true if path should be skipped based on exclude/include patterns.
// Include patterns (if present) act as a whitelist.
func shouldExclude(path string, excludePatterns []string, includePatterns []string) bool {
	// If include patterns are specified, treat as whitelist.
	if len(includePatterns) > 0 {
		included := false
		for _, pat := range includePatterns {
			match, _ := doublestar.Match(pat, path)
			if match {
				included = true
				break
			}
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

	// Collect explicit negations (patterns starting with "!")
	negations := []string{}
	for _, pat := range excludePatterns {
		if strings.HasPrefix(pat, "!") {
			negations = append(negations, strings.TrimPrefix(pat, "!"))
		}
	}

	// Check exclude patterns first.
	for _, pat := range excludePatterns {
		if pat == "" {
			continue
		}
		if strings.HasPrefix(pat, "!") {
			// will be handled later
			continue
		}
		// Try glob match against path and basename.
		if ok, _ := doublestar.Match(pat, path); ok {
			return true
		}
		if ok, _ := doublestar.Match(pat, filepath.Base(path)); ok {
			return true
		}
		// Fallback to substring match for convenience.
		if strings.Contains(path, pat) {
			return true
		}
	}

	// Apply negations: if any negation matches, do not exclude.
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

// readGitignore reads .gitignore lines (non-empty, non-comment).
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
		res = append(res, ln)
	}
	return res
}

// estimateTokens returns a rough token estimate based on total character length.
// Heuristic: 1 token ≈ 4 characters.
func estimateTokens(ctx *Context) int {
	totalChars := len(ctx.TreeStructure)
	for _, f := range ctx.Files {
		totalChars += len(f.Path) + len(f.Content)
		if f.AST != nil {
			totalChars += len(strings.Join(f.AST.Functions, ",")) + len(strings.Join(f.AST.Structs, ","))
		}
	}
	return totalChars / 4
}

// generateOutput serializes ctx into the requested format.
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

// generateMarkdown creates a human-friendly markdown summary containing
// the directory tree, per-file sections (AST summary if available), and content blocks.
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

	// Group files by language for easier navigation.
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
		// sort by path for stable output
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

	// Footer with generation timestamp.
	b.WriteString(fmt.Sprintf("_Generated by Contextify on %s_\n", time.Now().UTC().Format(time.RFC3339)))
	return b.String(), nil
}

// loadConfigFile merges a YAML config file into cfg without overwriting CLI values.
func loadConfigFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return err
	}
	// Merge with precedence: CLI > config file.
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

// trimFilesToTokenLimit tries to select a subset of files that fits within tokenLimit.
// It sorts by weight (high to low) and size (small to large) to preferentially keep
// small but important files.
func trimFilesToTokenLimit(ctx *Context, tokenLimit int) ([]FileInfo, bool) {
	// Copy slice and sort.
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
			// skip file if it would exceed the limit
			continue
		}
		out = append(out, f)
		acc += toks
	}
	truncated := len(out) < len(ctx.Files)
	return out, truncated
}
