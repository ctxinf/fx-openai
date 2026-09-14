// Package config reads the small TOML configuration surface used by fx-openai.
// It intentionally supports only the scalar string keys this program owns.
package config

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type File struct {
	Listen      string
	BaseURL     string
	BaseURLFile string
	APIKey      string
	APIKeyFile  string
	Model       string
}

const repoName = "fx-openai"

func Load(path string) (File, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	defer f.Close()

	var out File
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return File{}, fmt.Errorf("config %s:%d: expected key = value", path, lineNo)
		}
		key = normalizeKey(strings.TrimSpace(key))
		if key == "" {
			return File{}, fmt.Errorf("config %s:%d: empty key", path, lineNo)
		}
		parsed, err := parseString(strings.TrimSpace(value))
		if err != nil {
			return File{}, fmt.Errorf("config %s:%d: %w", path, lineNo, err)
		}
		switch key {
		case "listen":
			out.Listen = parsed
		case "baseurl", "upstream", "openaibaseurl":
			out.BaseURL = parsed
		case "baseurlfile":
			out.BaseURLFile = parsed
		case "apikey":
			out.APIKey = parsed
		case "apikeyfile":
			out.APIKeyFile = parsed
		case "model":
			out.Model = parsed
			// Unknown keys are ignored so the file can grow without breaking old binaries.
		}
	}
	if err := scanner.Err(); err != nil {
		return File{}, err
	}
	return out, nil
}

func ResolvePath(configPath, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(filepath.Dir(configPath), value)
}

func ReadValue(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func DecodeAPIKey(value string) (key string, encrypted bool, err error) {
	if value == "" {
		return "", false, nil
	}
	if strings.HasPrefix(value, "plain:") {
		return strings.TrimPrefix(value, "plain:"), false, nil
	}
	if !strings.HasPrefix(value, "secret:") {
		return value, false, nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "secret:"))
	if err != nil {
		return "", true, fmt.Errorf("decode secret: %w", err)
	}
	block, err := aes.NewCipher(secretKey())
	if err != nil {
		return "", true, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", true, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return "", true, fmt.Errorf("decode secret: ciphertext is too short")
	}
	plaintext, err := gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
	if err != nil {
		return "", true, fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), true, nil
}

func EncodeAPIKey(key string) (string, error) {
	if key == "" {
		return "", nil
	}
	block, err := aes.NewCipher(secretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(key), nil)
	return "secret:" + base64.StdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func UpdateAPIKey(path, value string) error {
	return UpdateValue(path, "api_key", value)
}

func UpdateValue(path, fieldName, value string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(contents), "\n")
	found := false
	for i, line := range lines {
		withoutNewline := strings.TrimSuffix(line, "\n")
		parsedKey, _, ok := strings.Cut(withoutNewline, "=")
		if !ok || normalizeKey(strings.TrimSpace(parsedKey)) != normalizeKey(fieldName) {
			continue
		}
		lines[i] = fieldName + " = " + strconv.Quote(value) + "\n"
		found = true
		break
	}
	if !found {
		if len(lines) > 0 && lines[len(lines)-1] != "" && !strings.HasSuffix(lines[len(lines)-1], "\n") {
			lines = append(lines, "\n")
		}
		lines = append(lines, fieldName+" = "+strconv.Quote(value)+"\n")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func RemoveKeys(path string, keys ...string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	remove := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		remove[normalizeKey(key)] = struct{}{}
	}
	lines := strings.SplitAfter(string(contents), "\n")
	kept := lines[:0]
	for _, line := range lines {
		withoutNewline := strings.TrimSuffix(line, "\n")
		field, _, ok := strings.Cut(withoutNewline, "=")
		if ok {
			if _, found := remove[normalizeKey(strings.TrimSpace(field))]; found {
				continue
			}
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "")), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func secretKey() []byte {
	sum := sha256.Sum256([]byte(repoName))
	return sum[:]
}

func normalizeKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	return key
}

func parseString(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", fmt.Errorf("value must be a quoted string")
	}
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("invalid string: %w", err)
	}
	return parsed, nil
}

func stripComment(line string) string {
	quoted := byte(0)
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quoted == '"' && escaped {
			escaped = false
			continue
		}
		if quoted == '"' && c == '\\' {
			escaped = true
			continue
		}
		if (c == '"' || c == '\'') && (quoted == 0 || quoted == c) {
			if quoted == 0 {
				quoted = c
			} else {
				quoted = 0
			}
			continue
		}
		if c == '#' && quoted == 0 {
			return line[:i]
		}
	}
	return line
}
