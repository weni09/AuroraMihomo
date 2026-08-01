package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSpaFileSystemServer(t *testing.T) {
	tmpDir := t.TempDir()
	indexPath := filepath.Join(tmpDir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html>head</html>"), 0644); err != nil {
		t.Fatalf("failed to write test index.html: %v", err)
	}

	testFilePath := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFilePath, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test.txt: %v", err)
	}

	handler := spaFileSystemServer("", http.Dir(tmpDir))

	// 1. Test existing file
	req := httptest.NewRequest(http.MethodGet, "/test.txt", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for existing file, got %d", w.Code)
	}
	if w.Body.String() != "hello world" {
		t.Errorf("expected body 'hello world', got '%s'", w.Body.String())
	}

	// 2. Test unknown route (SPA fallback to index.html)
	reqSPA := httptest.NewRequest(http.MethodGet, "/unknown/route", nil)
	wSPA := httptest.NewRecorder()
	handler.ServeHTTP(wSPA, reqSPA)

	if wSPA.Code != http.StatusOK {
		t.Errorf("expected status 200 for SPA fallback, got %d", wSPA.Code)
	}
	if wSPA.Body.String() != "<html>head</html>" {
		t.Errorf("expected body '<html>head</html>', got '%s'", wSPA.Body.String())
	}
}
