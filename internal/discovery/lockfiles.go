package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xdlc-labs/airlock/internal/manifest"
	"gopkg.in/yaml.v3"
)

// ponytail: line/regex parsers for lockfiles. Use ecosystem-native parsers if
// a format grows fields we actually hash.

var (
	pkgNameRE = regexp.MustCompile(`(?m)^name = "([^"]+)"`)
	pkgVerRE  = regexp.MustCompile(`(?m)^version = "([^"]+)"`)
	yarnVerRE = regexp.MustCompile(`(?m)^\s+version:?\s+"?([^"\s]+)"?`)
	yarnKeyRE = regexp.MustCompile(`^"?(.+?)"?:$`)
)

func scanLockfileDeps(root string, m *manifest.Manifest) error {
	for _, scan := range []func(string, *manifest.Manifest) error{
		scanGoSum,
		scanPackageLock,
		scanPnpmLock,
		scanYarnLock,
		scanCargoLock,
		scanPoetryLock,
		scanPipfileLock,
		scanUVLock,
	} {
		if err := scan(root, m); err != nil {
			return err
		}
	}
	return nil
}

func scanGoSum(root string, m *manifest.Manifest) error {
	path := filepath.Join(root, "go.sum")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m.Sources = append(m.Sources, manifest.Source{Kind: "go.sum", Path: "go.sum"})
	seen := depIndex(m)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mod, ver := fields[0], fields[1]
		if strings.Contains(mod, "/go.mod ") {
			continue
		}
		h := fields[len(fields)-1]
		if !strings.HasPrefix(h, "h1:") && len(fields) >= 3 {
			h = manifest.HashString(mod + "|" + ver + "|" + fields[2])
		}
		addDep(m, seen, slug(mod), "go", ver, "go.sum", h)
	}
	return nil
}

func scanPackageLock(root string, m *manifest.Manifest) error {
	for _, name := range []string{"package-lock.json", "npm-shrinkwrap.json"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		m.Sources = append(m.Sources, manifest.Source{Kind: "package-lock", Path: name})
		var lock struct {
			Packages map[string]struct {
				Version string `json:"version"`
				Name    string `json:"name"`
			} `json:"packages"`
			Dependencies map[string]struct {
				Version string `json:"version"`
			} `json:"dependencies"`
		}
		if err := json.Unmarshal(data, &lock); err != nil {
			return err
		}
		seen := depIndex(m)
		for pkgPath, pkg := range lock.Packages {
			if pkgPath == "" {
				continue
			}
			n := pkg.Name
			if n == "" {
				n = strings.TrimPrefix(pkgPath, "node_modules/")
			}
			addDep(m, seen, slug(n), "npm", pkg.Version, name, "")
		}
		for n, pkg := range lock.Dependencies {
			addDep(m, seen, slug(n), "npm", pkg.Version, name, "")
		}
		return nil
	}
	return nil
}

func scanPnpmLock(root string, m *manifest.Manifest) error {
	const name = "pnpm-lock.yaml"
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var lock struct {
		Packages map[string]any `yaml:"packages"`
	}
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return err
	}
	m.Sources = append(m.Sources, manifest.Source{Kind: "pnpm-lock", Path: name})
	seen := depIndex(m)
	for key := range lock.Packages {
		n, ver := splitPnpmKey(key)
		addDep(m, seen, slug(n), "npm", ver, name, "")
	}
	return nil
}

func scanYarnLock(root string, m *manifest.Manifest) error {
	const name = "yarn.lock"
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m.Sources = append(m.Sources, manifest.Source{Kind: "yarn.lock", Path: name})
	seen := depIndex(m)
	var key string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			key = ""
			if strings.HasPrefix(line, "__metadata") {
				continue
			}
			if km := yarnKeyRE.FindStringSubmatch(strings.TrimSpace(line)); len(km) == 2 {
				key = km[1]
			}
			continue
		}
		if key == "" {
			continue
		}
		if vm := yarnVerRE.FindStringSubmatch(line); len(vm) == 2 {
			addDep(m, seen, slug(yarnPackageName(key)), "npm", vm[1], name, "")
			key = ""
		}
	}
	return nil
}

func scanCargoLock(root string, m *manifest.Manifest) error {
	return scanTOMLPackages(root, "Cargo.lock", "cargo.lock", "cargo", m)
}

func scanPoetryLock(root string, m *manifest.Manifest) error {
	return scanTOMLPackages(root, "poetry.lock", "poetry.lock", "pypi", m)
}

func scanUVLock(root string, m *manifest.Manifest) error {
	return scanTOMLPackages(root, "uv.lock", "uv.lock", "pypi", m)
}

func scanTOMLPackages(root, filename, kind, eco string, m *manifest.Manifest) error {
	data, err := os.ReadFile(filepath.Join(root, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	m.Sources = append(m.Sources, manifest.Source{Kind: kind, Path: filename})
	seen := depIndex(m)
	for _, block := range strings.Split(string(data), "[[package]]") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		nameM := pkgNameRE.FindStringSubmatch(block)
		verM := pkgVerRE.FindStringSubmatch(block)
		if len(nameM) < 2 || len(verM) < 2 {
			continue
		}
		addDep(m, seen, slug(nameM[1]), eco, verM[1], filename, "")
	}
	return nil
}

func scanPipfileLock(root string, m *manifest.Manifest) error {
	const name = "Pipfile.lock"
	data, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Sources = append(m.Sources, manifest.Source{Kind: "pipfile.lock", Path: name})
	seen := depIndex(m)
	for section, body := range raw {
		if section == "_meta" {
			continue
		}
		var pkgs map[string]struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(body, &pkgs); err != nil {
			continue
		}
		for n, pkg := range pkgs {
			ver := strings.TrimLeft(pkg.Version, "=")
			addDep(m, seen, slug(n), "pypi", ver, name, "")
		}
	}
	return nil
}

func addDep(m *manifest.Manifest, seen map[string]bool, id, eco, ver, source, hash string) {
	if id == "" || ver == "" || seen[id] {
		return
	}
	if hash == "" {
		hash = manifest.HashString(id + "|" + ver)
	}
	m.Dependencies = append(m.Dependencies, manifest.Dependency{
		ID: id, Ecosystem: eco, Version: ver, Hash: hash, Source: source,
	})
	seen[id] = true
}

func splitPnpmKey(key string) (name, ver string) {
	key = strings.TrimPrefix(key, "/")
	if i := strings.IndexByte(key, '('); i >= 0 {
		key = key[:i]
	}
	idx := strings.LastIndex(key, "@")
	if idx <= 0 {
		return "", ""
	}
	return key[:idx], key[idx+1:]
}

func yarnPackageName(key string) string {
	first := strings.TrimSpace(strings.Split(key, ",")[0])
	first = strings.Trim(first, `"`)
	first = strings.Replace(first, "@npm:", "@", 1)
	if strings.HasPrefix(first, "@") {
		rest := first[1:]
		i := strings.Index(rest, "@")
		if i < 0 {
			return first
		}
		return "@" + rest[:i]
	}
	i := strings.Index(first, "@")
	if i < 0 {
		return first
	}
	return first[:i]
}

func depIndex(m *manifest.Manifest) map[string]bool {
	seen := map[string]bool{}
	for _, d := range m.Dependencies {
		seen[d.ID] = true
	}
	return seen
}
