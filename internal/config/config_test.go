package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".skillci.yaml")
	content := "models: [claude-sonnet-5, claude-opus-4-8]\nfail_on: any_fail\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Models) != 2 || cfg.Models[0] != "claude-sonnet-5" {
		t.Errorf("Models = %v, want [claude-sonnet-5 claude-opus-4-8]", cfg.Models)
	}
	if cfg.FailOn != "any_fail" {
		t.Errorf("FailOn = %q, want any_fail", cfg.FailOn)
	}
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	def := Default()
	if cfg.FailOn != def.FailOn || len(cfg.Models) != len(def.Models) {
		t.Errorf("Load(missing) = %+v, want default %+v", cfg, def)
	}
}
