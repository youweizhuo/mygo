package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"text/template"
)

var (
	templateDirOnce sync.Once
	templateDir     string
	templateDirErr  error

	verilatorTemplateOnce sync.Once
	verilatorTemplate     *template.Template
	verilatorTemplateErr  error
)

type verilatorDriverData struct {
	MaxCycles   int
	ResetCycles int
}

func renderVerilatorDriver(maxCycles, resetCycles int) (string, error) {
	tmpl, err := loadVerilatorTemplate()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	data := verilatorDriverData{
		MaxCycles:   maxCycles,
		ResetCycles: resetCycles,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func loadVerilatorTemplate() (*template.Template, error) {
	verilatorTemplateOnce.Do(func() {
		dir, err := templatesRoot()
		if err != nil {
			verilatorTemplateErr = err
			return
		}
		path := filepath.Join(dir, "verilator_driver.cpp.tmpl")
		verilatorTemplate, verilatorTemplateErr = template.ParseFiles(path)
	})
	return verilatorTemplate, verilatorTemplateErr
}

func templatesRoot() (string, error) {
	templateDirOnce.Do(func() {
		if env := os.Getenv("MYGO_TEMPLATE_DIR"); env != "" {
			templateDir = env
			return
		}
		candidates := []string{
			filepath.Join(repoDirFromSource(), "templates"),
			filepath.Join(executableDir(), "templates"),
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				templateDir = candidate
				return
			}
		}
		templateDirErr = fmt.Errorf("templates directory not found; set MYGO_TEMPLATE_DIR")
	})
	if templateDir != "" {
		return templateDir, nil
	}
	return "", templateDirErr
}

func repoDirFromSource() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Dir(file)
}

func executableDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func installXargsShim(dir string) (string, error) {
	data, err := readTemplateFile("xargs_shim.py")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "xargs")
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return "", fmt.Errorf("write xargs shim: %w", err)
	}
	return path, nil
}

func readTemplateFile(name string) ([]byte, error) {
	dir, err := templatesRoot()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", name, err)
	}
	return data, nil
}
