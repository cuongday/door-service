package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignSessionOpenUsesEd25519CanonicalRequest(t *testing.T) {
	privateKeyPEM, publicKeyPEM, err := GenerateKeyPairPEM()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	signatureText, err := SignSessionOpen(privateKeyPEM, "service-1", "nonce-1")
	if err != nil {
		t.Fatalf("sign session open: %v", err)
	}

	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		t.Fatal("public key PEM is invalid")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse public key: %v", err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		t.Fatalf("public key type = %T, want Ed25519", parsed)
	}
	signature, err := base64.RawURLEncoding.DecodeString(signatureText)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	canonical := strings.Join([]string{"POST", "/api/m2m/session/open", "nonce-1", "service-1"}, "\n")
	if !ed25519.Verify(publicKey, []byte(canonical), signature) {
		t.Fatal("signature does not verify against VMS canonical request")
	}
}

func TestEnsureKeyPairMigratesLegacyECDSAKey(t *testing.T) {
	legacyKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate legacy key: %v", err)
	}
	legacyDER, err := x509.MarshalPKCS8PrivateKey(legacyKey)
	if err != nil {
		t.Fatalf("marshal legacy key: %v", err)
	}
	privatePath := filepath.Join(t.TempDir(), "private.pem")
	legacyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: legacyDER})
	if err := os.WriteFile(privatePath, legacyPEM, 0o600); err != nil {
		t.Fatalf("write legacy key: %v", err)
	}

	privateKeyPEM, publicKeyPEM, err := (M2MSigning{PrivateKeyPath: privatePath}).EnsureKeyPair()
	if err != nil {
		t.Fatalf("migrate key: %v", err)
	}
	if _, err := parsePrivateKey(privateKeyPEM); err != nil {
		t.Fatalf("migrated private key is not Ed25519: %v", err)
	}
	block, _ := pem.Decode([]byte(publicKeyPEM))
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse migrated public key: %v", err)
	}
	if _, ok := parsed.(ed25519.PublicKey); !ok {
		t.Fatalf("migrated public key type = %T, want Ed25519", parsed)
	}
}
