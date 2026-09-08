package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanGoSumDependencies(t *testing.T) {
	root := t.TempDir()
	sum := "github.com/foo/bar v1.2.3 h1:abc123\n"
	if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range m.Dependencies {
		if d.Ecosystem == "go" && d.Version == "v1.2.3" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected go.sum dependency, got %+v", m.Dependencies)
	}
}

func TestScanPackageLockDependencies(t *testing.T) {
	root := t.TempDir()
	lock := `{
  "packages": {
    "node_modules/left-pad": { "name": "left-pad", "version": "1.3.0" }
  }
}`
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range m.Dependencies {
		if d.ID == "left-pad" && d.Ecosystem == "npm" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected npm dependency, got %+v", m.Dependencies)
	}
}

func TestScanCargoLockDependencies(t *testing.T) {
	root := t.TempDir()
	lock := `[[package]]
name = "serde"
version = "1.0.200"
`
	if err := os.WriteFile(filepath.Join(root, "Cargo.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range m.Dependencies {
		if d.ID == "serde" && d.Ecosystem == "cargo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cargo dependency, got %+v", m.Dependencies)
	}
}

func TestScanLockfileDepsTable(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		body    string
		wantID  string
		wantEco string
		wantVer string
	}{
		{
			name:    "pnpm v9",
			file:    "pnpm-lock.yaml",
			body:    "lockfileVersion: '9.0'\npackages:\n  left-pad@1.3.0:\n    resolution: {integrity: sha512-abc}\n",
			wantID:  "left-pad",
			wantEco: "npm",
			wantVer: "1.3.0",
		},
		{
			name:    "pnpm v6 scoped",
			file:    "pnpm-lock.yaml",
			body:    "lockfileVersion: '6.0'\npackages:\n  /@scope/pkg@2.0.0:\n    resolution: {integrity: sha512-abc}\n",
			wantID:  "-scope-pkg",
			wantEco: "npm",
			wantVer: "2.0.0",
		},
		{
			name:    "yarn v1",
			file:    "yarn.lock",
			body:    "# yarn lockfile v1\n\nleft-pad@^1.3.0:\n  version \"1.3.0\"\n  resolved \"https://example.test/left-pad\"\n",
			wantID:  "left-pad",
			wantEco: "npm",
			wantVer: "1.3.0",
		},
		{
			name:    "yarn berry",
			file:    "yarn.lock",
			body:    "__metadata:\n  version: 6\n\n\"left-pad@npm:^1.3.0\":\n  version: 1.3.0\n  resolution: \"left-pad@npm:1.3.0\"\n",
			wantID:  "left-pad",
			wantEco: "npm",
			wantVer: "1.3.0",
		},
		{
			name:    "poetry",
			file:    "poetry.lock",
			body:    "[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\ndescription = \"HTTP\"\n",
			wantID:  "requests",
			wantEco: "pypi",
			wantVer: "2.31.0",
		},
		{
			name:    "pipfile",
			file:    "Pipfile.lock",
			body:    `{"_meta":{"hash":{"sha256":"abc"},"sources":[]},"default":{"requests":{"version":"==2.31.0"}}}`,
			wantID:  "requests",
			wantEco: "pypi",
			wantVer: "2.31.0",
		},
		{
			name:    "uv",
			file:    "uv.lock",
			body:    "version = 1\n\n[[package]]\nname = \"requests\"\nversion = \"2.31.0\"\n",
			wantID:  "requests",
			wantEco: "pypi",
			wantVer: "2.31.0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, c.file), []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			m, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range m.Dependencies {
				if d.ID == c.wantID && d.Ecosystem == c.wantEco && d.Version == c.wantVer {
					if d.Hash == "" {
						t.Fatalf("missing hash for %s", d.ID)
					}
					if d.Source != c.file {
						t.Fatalf("source %q, want %q", d.Source, c.file)
					}
					return
				}
			}
			t.Fatalf("want %s %s %s, got %+v", c.wantID, c.wantEco, c.wantVer, m.Dependencies)
		})
	}
}

func TestSplitPnpmKey(t *testing.T) {
	cases := []struct {
		in, name, ver string
	}{
		{"left-pad@1.3.0", "left-pad", "1.3.0"},
		{"/left-pad@1.3.0", "left-pad", "1.3.0"},
		{"@scope/pkg@2.0.0", "@scope/pkg", "2.0.0"},
		{"left-pad@1.3.0(@foo/bar@1.0.0)", "left-pad", "1.3.0"},
	}
	for _, c := range cases {
		name, ver := splitPnpmKey(c.in)
		if name != c.name || ver != c.ver {
			t.Fatalf("%q -> %q %q, want %q %q", c.in, name, ver, c.name, c.ver)
		}
	}
}
