package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

const sessionOpenPath = "/api/m2m/session/open"

func GenerateKeyPairPEM() (string, string, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate signing key pair: %w", err)
	}
	privateKeyPEM, err := marshalPrivateKey(privateKey)
	if err != nil {
		return "", "", err
	}
	publicKeyPEM, err := PublicKeyPEMFromPrivate(privateKeyPEM)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(privateKeyPEM), strings.TrimSpace(publicKeyPEM), nil
}

func PublicKeyPEMFromPrivate(privateKeyPEM string) (string, error) {
	privateKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	return marshalPublicKey(privateKey.Public().(ed25519.PublicKey))
}

func SignSessionOpen(privateKeyPEM, serviceID, nonce string) (string, error) {
	privateKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	canonical := strings.Join([]string{"POST", sessionOpenPath, nonce, serviceID}, "\n")
	signature := ed25519.Sign(privateKey, []byte(canonical))
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func parsePrivateKey(privateKeyPEM string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(privateKeyPEM)))
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not Ed25519")
	}
	return privateKey, nil
}

func marshalPrivateKey(privateKey ed25519.PrivateKey) (string, error) {
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}
	return strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))), nil
}

func marshalPublicKey(publicKey ed25519.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	return strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))), nil
}
