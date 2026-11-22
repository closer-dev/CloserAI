// Package archtest проверяет правила DDD-слоёв, чтобы они не размывались со временем:
//
//   - domain не зависит ни от чего, кроме kernel и стандартной библиотеки
//     (плюс uuid): ни HTTP, ни БД, ни других слоёв, ни других контекстов;
//   - app зависит только от своего domain, kernel и стандартной библиотеки;
//   - kernel зависит только от uuid и стандартной библиотеки;
//   - контексты не импортируют domain/app/infra чужих контекстов (исключение —
//     api/FromContext, RequireRole из auth: это опубликованный HTTP-интерфейс).
package archtest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "closerai/internal/"

// пакеты, которые считаются общими и разрешены всем контекстам
var shared = []string{module + "kernel", module + "platform"}

func TestLayers(t *testing.T) {
	root := filepath.Join("..")
	contexts, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range contexts {
		name := ctx.Name()
		if !ctx.IsDir() || name == "kernel" || name == "platform" || name == "archtest" {
			continue
		}
		dir := filepath.Join(root, name)
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			rel, _ := filepath.Rel(dir, path)
			layer := strings.Split(filepath.ToSlash(rel), "/")[0]
			for _, imp := range imports(t, path) {
				if msg := violation(name, layer, imp); msg != "" {
					t.Errorf("%s: импорт %q: %s", path, imp, msg)
				}
			}
			return nil
		})
	}
}

func violation(ctx, layer, imp string) string {
	own := module + ctx + "/"
	isProject := strings.HasPrefix(imp, "closerai/")

	switch layer {
	case "domain":
		if imp == "github.com/google/uuid" || imp == "github.com/stretchr/testify/require" {
			return ""
		}
		if imp == module+"kernel" {
			return ""
		}
		if isProject || strings.Contains(imp, ".") {
			return "domain может зависеть только от kernel, uuid и стандартной библиотеки"
		}
	case "app":
		if imp == "github.com/google/uuid" || imp == "github.com/stretchr/testify/require" {
			return ""
		}
		if imp == module+"kernel" || strings.HasPrefix(imp, own+"domain") {
			return ""
		}
		if isProject || strings.Contains(imp, ".") {
			return "app может зависеть только от своего domain, kernel, uuid и стандартной библиотеки"
		}
	}

	// межконтекстные зависимости
	if strings.HasPrefix(imp, module) && !strings.HasPrefix(imp, own) {
		for _, s := range shared {
			if strings.HasPrefix(imp, s) {
				return ""
			}
		}
		if imp == module+"auth/api" && layer == "api" {
			return "" // FromContext / RequireRole
		}
		return "контексты общаются через порты и сервисы, собранные в main, а не через импорт чужих пакетов"
	}
	return ""
}

func imports(t *testing.T, path string) []string {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(f.Imports))
	for _, i := range f.Imports {
		p, _ := strconv.Unquote(i.Path.Value)
		out = append(out, p)
	}
	return out
}

// TestKernel: общее ядро (типы и доменные события) не тянет за собой ни
// инфраструктуру, ни контексты — иначе его нельзя импортировать из domain.
func TestKernel(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "kernel", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		for _, imp := range imports(t, f) {
			if (imp != "github.com/google/uuid" && strings.Contains(imp, ".")) || strings.HasPrefix(imp, "closerai/") {
				t.Errorf("%s: импорт %q: kernel может зависеть только от uuid и стандартной библиотеки", f, imp)
			}
		}
	}
}
