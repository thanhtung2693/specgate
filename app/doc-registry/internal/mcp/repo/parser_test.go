package repo

import (
	"fmt"
	"testing"
)

func TestParseGoSymbols_Functions(t *testing.T) {
	t.Parallel()
	src := `package main

func hello() {
	_ = "hello"
}

func add(a, b int) int {
	return a + b
}
`
	symbols, lang, err := ParseSymbols("main.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if lang != "go" {
		t.Errorf("language = %q, want %q", lang, "go")
	}
	names := symbolNames(symbols)
	assertContains(t, names, "hello")
	assertContains(t, names, "add")
	for _, s := range symbols {
		if s.Name == "hello" && s.Kind != "function" {
			t.Errorf("hello kind = %q, want function", s.Kind)
		}
	}
}

func TestParseGoSymbols_Methods(t *testing.T) {
	t.Parallel()
	src := `package svc

type Server struct {
	addr string
}

func (s *Server) Start() error {
	return nil
}

func (s Server) Addr() string {
	return s.addr
}
`
	symbols, _, err := ParseSymbols("server.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	names := symbolNames(symbols)
	assertContains(t, names, "Server")
	assertContains(t, names, "Start")
	assertContains(t, names, "Addr")
	for _, s := range symbols {
		if s.Name == "Start" && s.Kind != "method" {
			t.Errorf("Start kind = %q, want method", s.Kind)
		}
		if s.Name == "Server" && s.Kind != "type" {
			t.Errorf("Server kind = %q, want type", s.Kind)
		}
	}
}

func TestParseGoSymbols_Interface(t *testing.T) {
	t.Parallel()
	src := `package repo

type Store interface {
	Get(id string) error
	Put(id string, data []byte) error
}
`
	symbols, _, err := ParseSymbols("store.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range symbols {
		if s.Name == "Store" {
			found = true
			if s.Kind != "interface" {
				t.Errorf("Store kind = %q, want interface", s.Kind)
			}
		}
	}
	if !found {
		t.Error("Store not found in symbols")
	}
}

func TestParseGoSymbols_ConstsVars(t *testing.T) {
	t.Parallel()
	src := `package config

const MaxRetries = 3

var DefaultTimeout = 30

const (
	ModeA = "a"
	ModeB = "b"
)
`
	symbols, _, err := ParseSymbols("config.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	names := symbolNames(symbols)
	assertContains(t, names, "MaxRetries")
	assertContains(t, names, "DefaultTimeout")
	assertContains(t, names, "ModeA")
	assertContains(t, names, "ModeB")
}

func TestParseSymbols_UnsupportedLanguage(t *testing.T) {
	t.Parallel()
	symbols, lang, err := ParseSymbols("main.rs", []byte("fn main() {}"))
	if err != nil {
		t.Fatal("should not error for unsupported language")
	}
	if lang != "rust" {
		t.Errorf("language = %q, want %q", lang, "rust")
	}
	if len(symbols) != 0 {
		t.Errorf("got %d symbols, want 0 for unsupported language", len(symbols))
	}
}

func TestParseSymbols_MaxSymbols(t *testing.T) {
	t.Parallel()
	src := "package big\n\n"
	for i := 0; i < 120; i++ {
		src += "func F" + itoa(i) + "() {}\n"
	}
	symbols, _, err := ParseSymbols("big.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) > 100 {
		t.Errorf("got %d symbols, want max 100", len(symbols))
	}
}

func symbolNames(symbols []Symbol) []string {
	names := make([]string, len(symbols))
	for i, s := range symbols {
		names[i] = s.Name
	}
	return names
}

func assertContains(t *testing.T, names []string, want string) {
	t.Helper()
	for _, n := range names {
		if n == want {
			return
		}
	}
	t.Errorf("symbol %q not found in %v", want, names)
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
