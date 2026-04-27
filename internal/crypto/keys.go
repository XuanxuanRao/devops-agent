package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// KeyPair 封装 Ed25519 公私钥对。
type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

const (
	privateKeyFile = "ed25519_private.key"
	publicKeyFile  = "ed25519_public.key"
)

// EnsureKeyPair 确保 keyDir 下存在一对可用的 Ed25519 密钥；若不存在则自动生成。
func EnsureKeyPair(keyDir string) (KeyPair, error) {
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return KeyPair{}, fmt.Errorf("create key dir: %w", err)
	}

	privPath := filepath.Join(keyDir, privateKeyFile)
	pubPath := filepath.Join(keyDir, publicKeyFile)

	if _, err := os.Stat(privPath); err == nil {
		priv, err := os.ReadFile(privPath)
		if err != nil {
			return KeyPair{}, fmt.Errorf("read private key: %w", err)
		}
		pub, err := os.ReadFile(pubPath)
		if err != nil {
			return KeyPair{}, fmt.Errorf("read public key: %w", err)
		}
		return KeyPair{Public: ed25519.PublicKey(pub), Private: ed25519.PrivateKey(priv)}, nil
	}

	// 不存在则生成
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}

	if err := os.WriteFile(privPath, priv, 0o600); err != nil {
		return KeyPair{}, fmt.Errorf("write private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pub, 0o600); err != nil {
		return KeyPair{}, fmt.Errorf("write public key: %w", err)
	}

	return KeyPair{Public: pub, Private: priv}, nil
}

// DeviceID 根据公钥指纹生成设备标识，用于和服务端对齐 device.id。
func DeviceID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	// 使用前 16 字节以避免过长。
	return hex.EncodeToString(sum[:16])
}
