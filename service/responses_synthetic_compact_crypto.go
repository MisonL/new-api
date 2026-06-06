package service

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func encryptSyntheticCompactSummaryForRecord(record model.SyntheticCompactStateRecord, summary string) (string, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "", errors.New("synthetic compact state summary is empty")
	}
	if len(summary) > syntheticCompactSummaryMax {
		return "", fmt.Errorf("synthetic compact summary exceeds max size: %d > %d", len(summary), syntheticCompactSummaryMax)
	}
	block, err := aes.NewCipher(syntheticCompactSummaryKey())
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
	sealed := gcm.Seal(nonce, nonce, []byte(summary), syntheticCompactSummaryAAD(record))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptSyntheticCompactSummaryForRecord(record model.SyntheticCompactStateRecord) (string, error) {
	ciphertext := string(record.SummaryCiphertext)
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", errors.New("synthetic compact state summary ciphertext is empty")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(syntheticCompactSummaryKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("synthetic compact state summary ciphertext is too short")
	}
	nonce := sealed[:gcm.NonceSize()]
	payload := sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, payload, syntheticCompactSummaryAAD(record))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func syntheticCompactSummaryAAD(record model.SyntheticCompactStateRecord) []byte {
	parts := []string{
		strings.TrimSpace(record.ID),
		strings.TrimSpace(record.Model),
		fmt.Sprintf("%d", record.UserID),
		fmt.Sprintf("%d", record.TokenID),
		strings.TrimSpace(record.Group),
	}
	buf := bytes.NewBuffer(nil)
	for _, part := range parts {
		n := uint32(len(part))
		buf.WriteByte(byte(n >> 24))
		buf.WriteByte(byte(n >> 16))
		buf.WriteByte(byte(n >> 8))
		buf.WriteByte(byte(n))
		buf.WriteString(part)
	}
	return buf.Bytes()
}

func syntheticCompactSummaryKey() []byte {
	sum := sha256.Sum256([]byte("new-api synthetic compact state:" + common.CryptoSecret))
	return sum[:]
}
