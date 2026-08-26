package handlers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAppJS_BalancedBrackets(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	appJSPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../web/static/js/app.js"))
	bytes, err := os.ReadFile(appJSPath)
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}

	content := string(bytes)
	var stack []rune
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false
	escaped := false

	line := 1
	col := 1

	for i := 0; i < len(content); i++ {
		r := rune(content[i])
		if r == '\n' {
			line++
			col = 1
			inLineComment = false
			escaped = false
			continue
		}
		col++

		if inLineComment {
			continue
		}
		if inBlockComment {
			if r == '*' && i+1 < len(content) && content[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}
		if inSingleQuote {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '\'' {
				inSingleQuote = false
			}
			continue
		}
		if inDoubleQuote {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '"' {
				inDoubleQuote = false
			}
			continue
		}
		if inBacktick {
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == '`' {
				inBacktick = false
			}
			continue
		}

		// Not in string or comment
		if r == '/' && i+1 < len(content) && content[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}
		if r == '/' && i+1 < len(content) && content[i+1] == '*' {
			inBlockComment = true
			i++
			continue
		}
		if r == '\'' {
			inSingleQuote = true
			continue
		}
		if r == '"' {
			inDoubleQuote = true
			continue
		}
		if r == '`' {
			inBacktick = true
			continue
		}

		switch r {
		case '(', '[', '{':
			stack = append(stack, r)
		case ')':
			if len(stack) == 0 || stack[len(stack)-1] != '(' {
				t.Fatalf("Unmatched ')' at line %d, col %d", line, col)
			}
			stack = stack[:len(stack)-1]
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				t.Fatalf("Unmatched ']' at line %d, col %d", line, col)
			}
			stack = stack[:len(stack)-1]
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				t.Fatalf("Unmatched '}' at line %d, col %d", line, col)
			}
			stack = stack[:len(stack)-1]
		}
	}

	if len(stack) > 0 {
		t.Fatalf("Unclosed delimiter at end of file: %c (stack size: %d)", stack[len(stack)-1], len(stack))
	}
}
