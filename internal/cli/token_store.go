package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const keychainServiceName = "aitask-cli"
const tokenStoreFileModeEnv = "AITASK_TOKEN_STORE"

type TokenStore struct {
	homeDir string
}

func NewTokenStore() (*TokenStore, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &TokenStore{homeDir: home}, nil
}

func (s *TokenStore) Save(serverURL string, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("token cannot be empty")
	}
	account := tokenAccount(serverURL)
	if runtime.GOOS == "darwin" && !forceFileTokenStore() {
		if err := s.saveToKeychain(account, token); err == nil {
			return nil
		}
	}
	return s.saveToFile(account, token)
}

func (s *TokenStore) Load(serverURL string) (string, error) {
	account := tokenAccount(serverURL)
	if runtime.GOOS == "darwin" && !forceFileTokenStore() {
		token, err := s.loadFromKeychain(account)
		if err == nil {
			return token, nil
		}
	}
	token, err := s.loadFromFile(account)
	if err != nil {
		return "", fmt.Errorf("agent token not found, run `aitask auth token import` or `aitask auth bind --code ...`")
	}
	return token, nil
}

func forceFileTokenStore() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(tokenStoreFileModeEnv)), "file")
}

func (s *TokenStore) filePath(account string) string {
	dir := filepath.Join(s.homeDir, ".aitask", "credentials")
	return filepath.Join(dir, account+".token")
}

func (s *TokenStore) saveToFile(account string, token string) error {
	path := s.filePath(account)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token+"\n"), 0o600)
}

func (s *TokenStore) loadFromFile(account string) (string, error) {
	path := s.filePath(account)
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(payload)), nil
}

func (s *TokenStore) saveToKeychain(account string, token string) error {
	cmd := exec.Command("security", "add-generic-password", "-a", account, "-s", keychainServiceName, "-w", token, "-U")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain save failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (s *TokenStore) loadFromKeychain(account string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-a", account, "-s", keychainServiceName, "-w")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func tokenAccount(serverURL string) string {
	value := strings.TrimSpace(strings.ToLower(serverURL))
	if value == "" {
		value = "http://127.0.0.1:8080"
	}
	hash := sha256.Sum256([]byte(value))
	return "server-" + hex.EncodeToString(hash[:8])
}
