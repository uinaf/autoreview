package target

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/uinaf/autoreview/internal/protocol"
)

func TestTruffleHogScannerSmoke(t *testing.T) {
	if os.Getenv("AUTOREVIEW_REAL_TRUFFLEHOG") != "1" {
		t.Skip("set AUTOREVIEW_REAL_TRUFFLEHOG=1 to exercise the installed scanner")
	}
	scanner, err := newTruffleHogScanner("")
	if err != nil {
		t.Fatal(err)
	}
	if err := scanner.Scan(context.Background(), []byte("AUTOREVIEW-BUNDLE-V1\nbenign scanner smoke input\n")); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
}

func TestTruffleHogScannerDetectsCredential(t *testing.T) {
	if os.Getenv("AUTOREVIEW_REAL_TRUFFLEHOG") != "1" {
		t.Skip("set AUTOREVIEW_REAL_TRUFFLEHOG=1 to exercise the installed scanner")
	}
	scanner, err := newTruffleHogScanner("")
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	payload := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	if err := scanner.Scan(context.Background(), payload); !errors.Is(err, ErrSecretFound) {
		t.Fatalf("Scan() error = %v, want ErrSecretFound", err)
	}
}

func TestCollectorRealRepositorySmoke(t *testing.T) {
	if os.Getenv("AUTOREVIEW_REAL_REPOSITORY") != "1" {
		t.Skip("set AUTOREVIEW_REAL_REPOSITORY=1 to freeze the current checkout")
	}
	collector, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	repository := filepath.Join("..", "..")
	bundle, err := collector.Freeze(context.Background(), repository, Request{
		Mode:     protocol.TargetLocal,
		Prompt:   "Review issue 3 target-boundary implementation.",
		MaxBytes: 2 << 20,
	})
	if errors.Is(err, ErrNoChanges) {
		t.Skip("current checkout has no local changes")
	}
	if err != nil {
		t.Fatalf("Freeze() error = %v", err)
	}
	if len(bundle.Target().Files) == 0 || len(bundle.Payload()) == 0 {
		t.Fatal("real repository bundle is empty")
	}
	if err := bundle.VerifyUnchanged(context.Background()); err != nil {
		t.Fatalf("VerifyUnchanged() error = %v", err)
	}
}

func TestCollectorDetectsCredentialInDeletedFile(t *testing.T) {
	if os.Getenv("AUTOREVIEW_REAL_TRUFFLEHOG") != "1" {
		t.Skip("set AUTOREVIEW_REAL_TRUFFLEHOG=1 to exercise the installed scanner")
	}
	repository := newRepository(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, repository, "deleted-key.txt", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))
	gitCommand(t, repository, "add", ".")
	gitCommand(t, repository, "commit", "-m", "credential fixture")
	if err := os.Remove(filepath.Join(repository, "deleted-key.txt")); err != nil {
		t.Fatal(err)
	}
	collector, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.Freeze(context.Background(), repository, Request{Mode: protocol.TargetLocal}); !errors.Is(err, ErrSecretFound) {
		t.Fatalf("Freeze() error = %v, want ErrSecretFound", err)
	}
}
