package frontend

import (
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	gopackages "golang.org/x/tools/go/packages"

	"mygo/internal/diag"
)

// LoadConfig configures how source files should be loaded before SSA
// translation. For Phase 1 we only need raw source filenames and optional
// build tags.
type LoadConfig struct {
	Sources   []string
	BuildTags []string
}

// LoadPackages loads the requested source files using LLGo's enhanced loader
// (see `third_party/llgo/tmp/README.md` under "Development tools") so we get
// the same caching/dedup behavior as their SSA pipeline.
func LoadPackages(cfg LoadConfig, reporter *diag.Reporter) ([]*gopackages.Package, *token.FileSet, error) {
	if len(cfg.Sources) == 0 {
		return nil, nil, fmt.Errorf("no source files were provided")
	}

	fset := token.NewFileSet()
	buildFlags := buildTagFlag(cfg.BuildTags)

	dir := workingDir(cfg.Sources[0])
	if dir != "" {
		if absDir, err := filepath.Abs(dir); err == nil {
			dir = absDir
		}
	}

	goCache, goModCache := localCacheDirs()
	env := append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"GOCACHE="+goCache,
		"GOMODCACHE="+goModCache,
	)

	loadCfg := &gopackages.Config{
		Mode:  gopackages.NeedName | gopackages.NeedSyntax | gopackages.NeedFiles | gopackages.NeedCompiledGoFiles | gopackages.NeedTypes | gopackages.NeedTypesInfo | gopackages.NeedImports | gopackages.NeedDeps | gopackages.NeedModule | gopackages.NeedTypesSizes,
		Fset:  fset,
		Env:   env,
		Tests: false,
	}
	if dir != "" {
		loadCfg.Dir = dir
	}

	if len(buildFlags) > 0 {
		loadCfg.BuildFlags = buildFlags
	}

	pkgs, err := gopackages.Load(loadCfg, ".")
	if err != nil {
		return nil, nil, err
	}

	reporter.SetFileSet(fset)

	var hadErrors bool
	for _, pkg := range pkgs {
		for _, loadErr := range pkg.Errors {
			reporter.Errorf("%s: %s", loadErr.Pos, loadErr.Msg)
			hadErrors = true
		}
	}

	if hadErrors {
		return nil, nil, fmt.Errorf("package loading failed")
	}

	return pkgs, fset, nil
}

func buildTagFlag(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	joined := strings.Join(tags, ",")
	if joined == "" {
		return nil
	}
	return []string{"-tags=" + joined}
}

func workingDir(sample string) string {
	if sample == "" {
		return ""
	}
	dir := filepath.Dir(sample)
	if dir == "." {
		return ""
	}
	return dir
}

func localCacheDirs() (string, string) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	root := filepath.Join(cwd, ".cache")
	goCache := filepath.Join(root, "go-build")
	goModCache := filepath.Join(root, "gomod")
	_ = os.MkdirAll(goCache, 0o755)
	_ = os.MkdirAll(goModCache, 0o755)
	return goCache, goModCache
}
