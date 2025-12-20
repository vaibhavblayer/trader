// Package security provides credential encryption, audit logging, and security controls.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	// EncryptionKeySize is the size of the AES-256 key in bytes.
	EncryptionKeySize = 32
	// SaltSize is the size of the salt for key derivation.
	SaltSize = 16
	// NonceSize is the size of the GCM nonce.
	NonceSize = 12
	// PBKDF2Iterations is the number of iterations for key derivation.
	PBKDF2Iterations = 100000
)

// CredentialManager handles secure storage and retrieval of credentials.
type CredentialManager struct {
	configDir    string
	masterKey    []byte
	credentials  *EncryptedCredentials
	mu           sync.RWMutex
	sessionStart time.Time
	timeout      time.Duration
}

// EncryptedCredentials holds encrypted credential data.
type EncryptedCredentials struct {
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	Version    int    `json:"version"`
}

// PlainCredentials holds decrypted credential data.
type PlainCredentials struct {
	Zerodha ZerodhaCredentials `json:"zerodha"`
	OpenAI  OpenAICredentials  `json:"openai"`
	Tavily  TavilyCredentials  `json:"tavily"`
}

// ZerodhaCredentials holds Zerodha API credentials.
type ZerodhaCredentials struct {
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
	UserID    string `json:"user_id"`
}

// OpenAICredentials holds OpenAI API credentials.
type OpenAICredentials struct {
	APIKey string `json:"api_key"`
}

// TavilyCredentials holds Tavily API credentials.
type TavilyCredentials struct {
	APIKey string `json:"api_key"`
}

// NewCredentialManager creates a new credential manager.
func NewCredentialManager(configDir string, sessionTimeout time.Duration) *CredentialManager {
	if sessionTimeout == 0 {
		sessionTimeout = 8 * time.Hour // Default 8-hour session
	}
	return &CredentialManager{
		configDir:    configDir,
		timeout:      sessionTimeout,
		sessionStart: time.Now(),
	}
}

// Initialize sets up the credential manager with a master password.
func (cm *CredentialManager) Initialize(masterPassword string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	encryptedPath := filepath.Join(cm.configDir, "credentials.enc")

	// Check if encrypted credentials exist
	if _, err := os.Stat(encryptedPath); os.IsNotExist(err) {
		// No encrypted credentials, check for plain text credentials to migrate
		plainPath := filepath.Join(cm.configDir, "credentials.toml")
		if _, err := os.Stat(plainPath); err == nil {
			return cm.migrateFromPlainText(masterPassword, plainPath, encryptedPath)
		}
		// No credentials at all, create empty encrypted file
		return cm.createEmptyCredentials(masterPassword, encryptedPath)
	}

	// Load and decrypt existing credentials
	return cm.loadEncryptedCredentials(masterPassword, encryptedPath)
}


// deriveKey derives an encryption key from a password using PBKDF2.
func deriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, PBKDF2Iterations, EncryptionKeySize, sha256.New)
}

// encrypt encrypts plaintext using AES-256-GCM.
func encrypt(plaintext, key []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonce = make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// decrypt decrypts ciphertext using AES-256-GCM.
func decrypt(ciphertext, key, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	return plaintext, nil
}

// migrateFromPlainText migrates plain text credentials to encrypted format.
func (cm *CredentialManager) migrateFromPlainText(masterPassword, plainPath, encryptedPath string) error {
	// Read plain text credentials
	data, err := os.ReadFile(plainPath)
	if err != nil {
		return fmt.Errorf("reading plain credentials: %w", err)
	}

	// Parse TOML to extract credentials
	creds := &PlainCredentials{}
	if err := parseTOMLCredentials(string(data), creds); err != nil {
		return fmt.Errorf("parsing credentials: %w", err)
	}

	// Encrypt and save
	if err := cm.saveCredentials(masterPassword, creds, encryptedPath); err != nil {
		return fmt.Errorf("saving encrypted credentials: %w", err)
	}

	// Securely delete plain text file
	if err := secureDelete(plainPath); err != nil {
		// Log warning but don't fail
		fmt.Printf("Warning: could not securely delete plain credentials file: %v\n", err)
	}

	return nil
}

// parseTOMLCredentials parses TOML credential content.
func parseTOMLCredentials(content string, creds *PlainCredentials) error {
	lines := strings.Split(content, "\n")
	currentSection := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.Trim(line, "[]")
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch currentSection {
		case "zerodha":
			switch key {
			case "api_key":
				creds.Zerodha.APIKey = value
			case "api_secret":
				creds.Zerodha.APISecret = value
			case "user_id":
				creds.Zerodha.UserID = value
			}
		case "openai":
			if key == "api_key" {
				creds.OpenAI.APIKey = value
			}
		case "tavily":
			if key == "api_key" {
				creds.Tavily.APIKey = value
			}
		}
	}

	return nil
}

// saveCredentials encrypts and saves credentials.
func (cm *CredentialManager) saveCredentials(masterPassword string, creds *PlainCredentials, path string) error {
	// Generate salt
	salt := make([]byte, SaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("generating salt: %w", err)
	}

	// Derive key
	key := deriveKey(masterPassword, salt)
	cm.masterKey = key

	// Serialize credentials
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("serializing credentials: %w", err)
	}

	// Encrypt
	nonce, ciphertext, err := encrypt(plaintext, key)
	if err != nil {
		return fmt.Errorf("encrypting credentials: %w", err)
	}

	// Create encrypted credentials structure
	encCreds := &EncryptedCredentials{
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
		Version:    1,
	}

	// Save to file
	data, err := json.MarshalIndent(encCreds, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing encrypted credentials: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Write with restricted permissions
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing encrypted credentials: %w", err)
	}

	cm.credentials = encCreds
	return nil
}

// loadEncryptedCredentials loads and decrypts credentials.
func (cm *CredentialManager) loadEncryptedCredentials(masterPassword, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading encrypted credentials: %w", err)
	}

	encCreds := &EncryptedCredentials{}
	if err := json.Unmarshal(data, encCreds); err != nil {
		return fmt.Errorf("parsing encrypted credentials: %w", err)
	}

	// Decode base64 values
	salt, err := base64.StdEncoding.DecodeString(encCreds.Salt)
	if err != nil {
		return fmt.Errorf("decoding salt: %w", err)
	}

	// Derive key and store it
	key := deriveKey(masterPassword, salt)
	cm.masterKey = key
	cm.credentials = encCreds
	cm.sessionStart = time.Now()

	// Verify by attempting to decrypt
	_, err = cm.GetCredentials()
	if err != nil {
		cm.masterKey = nil
		return fmt.Errorf("invalid master password")
	}

	return nil
}

// createEmptyCredentials creates an empty encrypted credentials file.
func (cm *CredentialManager) createEmptyCredentials(masterPassword, path string) error {
	creds := &PlainCredentials{}
	return cm.saveCredentials(masterPassword, creds, path)
}

// GetCredentials returns decrypted credentials.
func (cm *CredentialManager) GetCredentials() (*PlainCredentials, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.masterKey == nil || cm.credentials == nil {
		return nil, fmt.Errorf("credential manager not initialized")
	}

	// Check session timeout
	if time.Since(cm.sessionStart) > cm.timeout {
		return nil, fmt.Errorf("session expired, please re-authenticate")
	}

	// Decode values
	nonce, err := base64.StdEncoding.DecodeString(cm.credentials.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decoding nonce: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(cm.credentials.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding ciphertext: %w", err)
	}

	// Decrypt
	plaintext, err := decrypt(ciphertext, cm.masterKey, nonce)
	if err != nil {
		return nil, fmt.Errorf("decrypting credentials: %w", err)
	}

	// Parse
	creds := &PlainCredentials{}
	if err := json.Unmarshal(plaintext, creds); err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}

	return creds, nil
}

// UpdateCredentials updates and re-encrypts credentials.
func (cm *CredentialManager) UpdateCredentials(creds *PlainCredentials) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.masterKey == nil {
		return fmt.Errorf("credential manager not initialized")
	}

	// Check session timeout
	if time.Since(cm.sessionStart) > cm.timeout {
		return fmt.Errorf("session expired, please re-authenticate")
	}

	// Get current salt
	salt, err := base64.StdEncoding.DecodeString(cm.credentials.Salt)
	if err != nil {
		return fmt.Errorf("decoding salt: %w", err)
	}

	// Serialize credentials
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("serializing credentials: %w", err)
	}

	// Encrypt with new nonce
	nonce, ciphertext, err := encrypt(plaintext, cm.masterKey)
	if err != nil {
		return fmt.Errorf("encrypting credentials: %w", err)
	}

	// Update structure
	cm.credentials.Nonce = base64.StdEncoding.EncodeToString(nonce)
	cm.credentials.Ciphertext = base64.StdEncoding.EncodeToString(ciphertext)
	cm.credentials.Salt = base64.StdEncoding.EncodeToString(salt)

	// Save to file
	path := filepath.Join(cm.configDir, "credentials.enc")
	data, err := json.MarshalIndent(cm.credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing encrypted credentials: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing encrypted credentials: %w", err)
	}

	return nil
}

// IsSessionValid checks if the current session is still valid.
func (cm *CredentialManager) IsSessionValid() bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.masterKey != nil && time.Since(cm.sessionStart) <= cm.timeout
}

// RefreshSession refreshes the session timeout.
func (cm *CredentialManager) RefreshSession() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.sessionStart = time.Now()
}

// ClearSession clears the session and master key from memory.
func (cm *CredentialManager) ClearSession() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Securely clear the master key
	if cm.masterKey != nil {
		for i := range cm.masterKey {
			cm.masterKey[i] = 0
		}
		cm.masterKey = nil
	}
	cm.credentials = nil
}

// secureDelete overwrites a file with random data before deleting.
func secureDelete(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Open file for writing
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}

	// Overwrite with random data
	randomData := make([]byte, info.Size())
	if _, err := rand.Read(randomData); err != nil {
		f.Close()
		return err
	}

	if _, err := f.Write(randomData); err != nil {
		f.Close()
		return err
	}

	f.Close()

	// Delete the file
	return os.Remove(path)
}
