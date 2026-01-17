package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"encoding/pem"
)

type staticResolver map[string]string

func (sr staticResolver) ResolveCanonical(domain string) (string, bool) {
	canonical, ok := sr[domain]
	return canonical, ok
}

func TestCertManagerExactMatch(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, "example.com")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cm, err := NewCertManager(dir, logger)
	if err != nil {
		t.Fatalf("NewCertManager() error = %v", err)
	}

	if _, err := cm.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.com"}); err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
}

func TestCertManagerAliasMatch(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, "example.com")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cm, err := NewCertManager(dir, logger)
	if err != nil {
		t.Fatalf("NewCertManager() error = %v", err)
	}

	cm.SetDomainResolver(staticResolver{
		"alias.example.com": "example.com",
	})

	if _, err := cm.GetCertificate(&tls.ClientHelloInfo{ServerName: "alias.example.com"}); err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
}

func TestCertManagerWildcardMatch(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, "*.example.com")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cm, err := NewCertManager(dir, logger)
	if err != nil {
		t.Fatalf("NewCertManager() error = %v", err)
	}

	if _, err := cm.GetCertificate(&tls.ClientHelloInfo{ServerName: "app.example.com"}); err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}

	if _, err := cm.GetCertificate(&tls.ClientHelloInfo{ServerName: "app.dev.example.com"}); err == nil {
		t.Fatal("GetCertificate() expected error for multi-level wildcard match")
	}
}

func writeTestCert(t *testing.T, dir, domain string) {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("rand.Int() error = %v", err)
	}

	certTemplate := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: domain,
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		DNSNames: []string{domain},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &certTemplate, &certTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}

	certPath := filepath.Join(dir, domain+".pem")
	pemData := append(
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})...,
	)

	if err := writeFile(certPath, pemData); err != nil {
		t.Fatalf("writeFile() error = %v", err)
	}
}

func writeFile(path string, contents []byte) error {
	return os.WriteFile(path, contents, 0o600)
}
