package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type M2MSigning struct {
	PrivateKeyPath string
}

func (s M2MSigning) PublicKeyPath() string {
	return filepath.Join(filepath.Dir(s.PrivateKeyPath), "public.pem")
}

func (s M2MSigning) EnsureKeyPair() (string, string, error) {
	privateKeyPath := strings.TrimSpace(s.PrivateKeyPath)
	if privateKeyPath == "" {
		return "", "", fmt.Errorf("signing private key path is required")
	}
	if data, err := os.ReadFile(privateKeyPath); err == nil {
		privateKey := strings.TrimSpace(string(data))
		publicKeyPEM, err := PublicKeyPEMFromPrivate(privateKey)
		if err == nil {
			if err := os.WriteFile(s.PublicKeyPath(), []byte(strings.TrimSpace(publicKeyPEM)+"\n"), 0644); err != nil {
				return "", "", err
			}
			return privateKey, publicKeyPEM, nil
		}
	}
	privateKey, publicKeyPEM, err := GenerateKeyPairPEM()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(privateKeyPath), 0700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(privateKeyPath, []byte(strings.TrimSpace(privateKey)+"\n"), 0600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(s.PublicKeyPath(), []byte(strings.TrimSpace(publicKeyPEM)+"\n"), 0644); err != nil {
		return "", "", err
	}
	return privateKey, publicKeyPEM, nil
}
