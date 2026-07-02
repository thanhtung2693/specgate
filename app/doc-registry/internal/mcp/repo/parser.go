package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// MaxSymbolsPerFile is the max number of symbols returned per file.
const MaxSymbolsPerFile = 100

// Symbol represents a code symbol (function, type, etc.).
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// ParseSymbols extracts symbols from a source file. Returns the detected
// language and symbols. For unsupported languages, returns empty symbols
// with no error.
func ParseSymbols(filename string, content []byte) ([]Symbol, string, error) {
	lang := detectLanguage(filename)

	switch lang {
	case "go":
		symbols, err := parseGoSymbols(filename, content)
		if err != nil {
			return nil, lang, nil
		}
		if len(symbols) > MaxSymbolsPerFile {
			symbols = symbols[:MaxSymbolsPerFile]
		}
		return symbols, lang, nil
	default:
		return []Symbol{}, lang, nil
	}
}

func detectLanguage(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".md":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}

func parseGoSymbols(filename string, content []byte) ([]Symbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, content, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := "function"
			if d.Recv != nil {
				kind = "method"
			}
			start := fset.Position(d.Pos())
			end := fset.Position(d.End())
			symbols = append(symbols, Symbol{
				Name:      d.Name.Name,
				Kind:      kind,
				StartLine: start.Line,
				EndLine:   end.Line,
			})

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := "type"
					if _, ok := s.Type.(*ast.InterfaceType); ok {
						kind = "interface"
					}
					start := fset.Position(s.Pos())
					end := fset.Position(s.End())
					symbols = append(symbols, Symbol{
						Name:      s.Name.Name,
						Kind:      kind,
						StartLine: start.Line,
						EndLine:   end.Line,
					})

				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						if name.Name == "_" {
							continue
						}
						start := fset.Position(name.Pos())
						end := fset.Position(s.End())
						symbols = append(symbols, Symbol{
							Name:      name.Name,
							Kind:      kind,
							StartLine: start.Line,
							EndLine:   end.Line,
						})
					}
				}
			}
		}
	}

	return symbols, nil
}

// ExtractSymbolBody extracts the source text for a named symbol from content.
func ExtractSymbolBody(filename string, content []byte, symbolName string) (body string, sym *Symbol, found bool) {
	symbols, _, err := ParseSymbols(filename, content)
	if err != nil || len(symbols) == 0 {
		return "", nil, false
	}

	lines := strings.Split(string(content), "\n")
	for i := range symbols {
		s := symbols[i]
		if s.Name == symbolName {
			start := s.StartLine - 1
			end := s.EndLine
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			body := strings.Join(lines[start:end], "\n")
			return body, &s, true
		}
	}
	return "", nil, false
}
