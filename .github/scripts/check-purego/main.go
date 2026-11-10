// Command check-purego audits the server and baker dependency closure.
// It is development tooling and is not imported by ordinary application code.
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
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type dependency struct {
	ImportPath, Dir string
	Standard        bool
	CgoFiles        []string
}

func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check() error {
	args := []string{"list", "-deps", "-json", ".", "./bake", "./dynamic", "./artifact", "./recast", "./geom", "./loader", "./crowd", "./tilecache", "./fastlz", "./cmd/navmesh-bake", "./examples/headless"}
	// Reuse this process's toolchain even when launched through GOTOOLCHAIN.
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("list production dependencies: %w: %s", err, stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	count := 0
	for {
		var p dependency
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if forbiddenImport(p.ImportPath) || len(p.CgoFiles) != 0 {
			return fmt.Errorf("production dependency %q requires native execution or bindings", p.ImportPath)
		}
		if p.Standard {
			continue // The Go runtime's own assembly/system calls are expected.
		}
		entries, err := os.ReadDir(p.Dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue // In particular, development-only testdata is not code.
			}
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			switch ext {
			case ".c", ".cc", ".cpp", ".cxx", ".m", ".mm", ".f", ".f90", ".syso", ".swig", ".swigcxx", ".dll", ".so", ".dylib", ".a", ".lib":
				return fmt.Errorf("native source or binary in production dependency: %s", filepath.Join(p.Dir, name))
			}
			if ext != ".go" || strings.HasSuffix(name, "_test.go") {
				continue
			}
			// Inspect all platform variants, including files disabled by build
			// tags on the machine running this check.
			path := filepath.Join(p.Dir, name)
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				name, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				if forbiddenImport(name) {
					return fmt.Errorf("forbidden production import %q in %s", name, path)
				}
			}
			var nativeCall string
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if ok {
					switch sel.Sel.Name {
					case "LoadDLL", "MustLoadDLL", "NewLazyDLL", "NewLazySystemDLL", "LoadLibrary", "LoadLibraryEx", "Dlopen", "Dlsym":
						nativeCall = sel.Sel.Name
					}
				}
				return true
			})
			if nativeCall != "" {
				return fmt.Errorf("native-library entry point %s in %s", nativeCall, path)
			}
		}
		count++
	}
	fmt.Printf("Pure-Go server/baker closure checked: %d nonstandard packages; no native bindings or subprocess dependencies.\n", count)
	return nil
}

func forbiddenImport(path string) bool {
	return path == "C" || path == "os/exec" || path == "plugin" || path == "runtime/cgo" || strings.Contains(strings.ToLower(path), "purego")
}
