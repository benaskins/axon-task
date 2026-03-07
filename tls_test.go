package task

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- RequireClientCert tests ---

func TestRequireClientCert_NoTLS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called")
	})

	handler := RequireClientCert(inner)
	req := httptest.NewRequest("GET", "/", nil)
	// req.TLS is nil by default
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestRequireClientCert_NoPeerCerts(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called")
	})

	handler := RequireClientCert(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{}}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireClientCert_WithCert(t *testing.T) {
	var gotCN string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cn, ok := r.Context().Value(ClientCNKey).(string)
		if !ok {
			t.Error("expected ClientCNKey in context")
			return
		}
		gotCN = cn
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireClientCert(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{
			{Subject: pkix.Name{CommonName: "test-agent"}},
		},
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotCN != "test-agent" {
		t.Errorf("expected CN test-agent, got %q", gotCN)
	}
}

// --- LoadTLSConfig tests ---

// generateTestCACert creates a self-signed CA certificate PEM for testing.
func generateTestCACert(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestLoadTLSConfig_ValidCA(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, generateTestCACert(t), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadTLSConfig(caPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil TLS config")
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Error("expected RequireAndVerifyClientCert")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Error("expected MinVersion TLS 1.2")
	}
}

func TestLoadTLSConfig_InvalidPath(t *testing.T) {
	_, err := LoadTLSConfig("/nonexistent/ca.pem")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestLoadTLSConfig_InvalidPEM(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(caPath, []byte("not a valid PEM"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadTLSConfig(caPath)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

// --- Executor.Store() accessor ---

func TestExecutorStoreAccessor(t *testing.T) {
	store := newMemoryStore()
	executor := NewExecutor("claude", "/tmp", "test", store)
	defer executor.Shutdown()

	if executor.Store() != store {
		t.Error("Store() should return the store passed to NewExecutor")
	}
}
