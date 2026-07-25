package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabirnarang39/skillci/internal/history"
)

func TestBadgeCmdErrorsWithNoHistory(t *testing.T) {
	dir := t.TempDir()

	cmd := newBadgeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{dir})

	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want error when no history.json exists")
	}
}

func TestBadgeCmdWritesPassingSVGWhenAllCasesPassed(t *testing.T) {
	dir := t.TempDir()
	h := history.History{}
	h.Append(history.Run{CommitSHA: "abc123", Cases: []history.CaseResult{
		{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
		{Name: "case-b", Model: "claude-sonnet-5", Passed: true},
	}})
	if err := h.Save(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Fatal(err)
	}

	cmd := newBadgeCmd()
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	svg, err := os.ReadFile(filepath.Join(dir, ".skillci", "badge.svg"))
	if err != nil {
		t.Fatalf("reading badge.svg: %v", err)
	}
	if !strings.Contains(string(svg), "passing") {
		t.Errorf("badge.svg = %s, want it to say passing", svg)
	}
	if !strings.Contains(string(svg), "#2ea44f") {
		t.Errorf("badge.svg = %s, want the passing green color", svg)
	}
}

func TestBadgeCmdWritesRegressedSVGWhenAllCasesFailed(t *testing.T) {
	dir := t.TempDir()
	h := history.History{}
	h.Append(history.Run{CommitSHA: "abc123", Cases: []history.CaseResult{
		{Name: "case-a", Model: "claude-sonnet-5", Passed: false},
	}})
	if err := h.Save(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Fatal(err)
	}

	cmd := newBadgeCmd()
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	svg, err := os.ReadFile(filepath.Join(dir, ".skillci", "badge.svg"))
	if err != nil {
		t.Fatalf("reading badge.svg: %v", err)
	}
	if !strings.Contains(string(svg), "regressed") {
		t.Errorf("badge.svg = %s, want it to say regressed", svg)
	}
}

func TestBadgeCmdWritesPartialSVGWhenSomeCasesFailed(t *testing.T) {
	dir := t.TempDir()
	h := history.History{}
	h.Append(history.Run{CommitSHA: "abc123", Cases: []history.CaseResult{
		{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
		{Name: "case-b", Model: "claude-sonnet-5", Passed: false},
	}})
	if err := h.Save(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Fatal(err)
	}

	cmd := newBadgeCmd()
	cmd.SetArgs([]string{dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	svg, err := os.ReadFile(filepath.Join(dir, ".skillci", "badge.svg"))
	if err != nil {
		t.Fatalf("reading badge.svg: %v", err)
	}
	if !strings.Contains(string(svg), "partial") {
		t.Errorf("badge.svg = %s, want it to say partial", svg)
	}
}

func TestBadgeCmdDefaultsPathToCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	h := history.History{}
	h.Append(history.Run{CommitSHA: "abc123", Cases: []history.CaseResult{
		{Name: "case-a", Model: "claude-sonnet-5", Passed: true},
	}})
	if err := h.Save(filepath.Join(dir, ".skillci", "history.json")); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWD)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cmd := newBadgeCmd()
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".skillci", "badge.svg")); err != nil {
		t.Errorf("badge.svg not written in current directory: %v", err)
	}
}
