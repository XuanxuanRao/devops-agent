package internal

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// 生成临时的 RSA 密钥对用于测试
func generateTestKeys() (string, string, error) {
	// 生成密钥对
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	// 创建私钥 PEM
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	})

	// 创建公钥 PEM
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", err
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	})

	// 写入临时文件
	privateKeyPath := "/tmp/test_private.key"
	publicKeyPath := "/tmp/test_public.key"

	err = os.WriteFile(privateKeyPath, privateKeyPEM, 0644)
	if err != nil {
		return "", "", err
	}

	err = os.WriteFile(publicKeyPath, publicKeyPEM, 0644)
	if err != nil {
		return "", "", err
	}

	return privateKeyPath, publicKeyPath, nil
}

// 清理测试文件
func cleanupTestKeys(privateKeyPath, publicKeyPath string) {
	os.Remove(privateKeyPath)
	os.Remove(publicKeyPath)
}

// Test_RSAMessageSigner_Sign_Correct 测试正确的签名生成
func Test_RSAMessageSigner_Sign_Correct(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)
	assert.True(t, signer.Enabled())

	// 测试签名生成
	hostname := "test-host"
	signature, timestamp, err := signer.Sign(hostname)
	assert.NoError(t, err)
	assert.NotEmpty(t, signature)
	assert.Greater(t, timestamp, int64(0))
}

// Test_RSAMessageSigner_Sign_Disabled 测试禁用签名时的行为
func Test_RSAMessageSigner_Sign_Disabled(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建禁用的签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, false)
	assert.NoError(t, err)
	assert.False(t, signer.Enabled())

	// 测试禁用时的签名生成
	hostname := "test-host"
	signature, timestamp, err := signer.Sign(hostname)
	assert.NoError(t, err)
	assert.Empty(t, signature)
	assert.Equal(t, int64(0), timestamp)
}

// Test_RSAMessageSigner_Sign_NoPrivateKey 测试无私钥时的行为
func Test_RSAMessageSigner_Sign_NoPrivateKey(t *testing.T) {
	// 生成测试密钥
	_, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys("", publicKeyPath)

	// 创建无私钥的签名器
	signer, err := NewRSAMessageSigner("", publicKeyPath, true)
	assert.NoError(t, err)
	assert.True(t, signer.Enabled())

	// 测试无私钥时的签名生成
	hostname := "test-host"
	signature, timestamp, err := signer.Sign(hostname)
	assert.NoError(t, err)
	assert.Empty(t, signature)
	assert.Equal(t, int64(0), timestamp)
}

// Test_RSAMessageSigner_Verify_Correct 测试正确的签名验证
func Test_RSAMessageSigner_Verify_Correct(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 生成签名
	hostname := "test-host"
	signature, timestamp, err := signer.Sign(hostname)
	assert.NoError(t, err)
	assert.NotEmpty(t, signature)

	// 验证签名
	valid, err := signer.Verify(hostname, signature, timestamp)
	assert.NoError(t, err)
	assert.True(t, valid)
}

// Test_RSAMessageSigner_Verify_Invalid 测试无效签名的验证
func Test_RSAMessageSigner_Verify_Invalid(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 测试无效签名
	hostname := "test-host"
	_, timestamp, err := signer.Sign(hostname)
	assert.NoError(t, err)

	// 使用无效签名
	valid, err := signer.Verify(hostname, "invalid-signature", timestamp)
	assert.Error(t, err)
	assert.False(t, valid)
}

// Test_RSAMessageSigner_Verify_WrongHostname 测试错误主机名的验证
func Test_RSAMessageSigner_Verify_WrongHostname(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 生成签名
	originalHostname := "test-host"
	signature, timestamp, err := signer.Sign(originalHostname)
	assert.NoError(t, err)

	// 使用错误的主机名验证
	wrongHostname := "wrong-host"
	valid, err := signer.Verify(wrongHostname, signature, timestamp)
	assert.Error(t, err)
	assert.False(t, valid)
}

// Test_RSAMessageSigner_Verify_WrongTimestamp 测试错误时间戳的验证
func Test_RSAMessageSigner_Verify_WrongTimestamp(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 生成签名
	hostname := "test-host"
	signature, originalTimestamp, err := signer.Sign(hostname)
	assert.NoError(t, err)

	// 使用错误的时间戳验证
	wrongTimestamp := originalTimestamp + 1
	valid, err := signer.Verify(hostname, signature, wrongTimestamp)
	assert.Error(t, err)
	assert.False(t, valid)
}

// Test_RSAMessageSigner_Verify_Disabled 测试禁用签名时的验证行为
func Test_RSAMessageSigner_Verify_Disabled(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建禁用的签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, false)
	assert.NoError(t, err)
	assert.False(t, signer.Enabled())

	// 测试禁用时的签名验证
	hostname := "test-host"
	valid, err := signer.Verify(hostname, "any-signature", time.Now().Unix())
	assert.NoError(t, err)
	assert.True(t, valid)
}

// Test_RSAMessageSigner_Verify_NoPublicKey 测试无公钥时的验证行为
func Test_RSAMessageSigner_Verify_NoPublicKey(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, _, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, "")

	// 创建无公钥的签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, "", true)
	assert.NoError(t, err)
	assert.True(t, signer.Enabled())

	// 测试无公钥时的签名验证
	hostname := "test-host"
	valid, err := signer.Verify(hostname, "any-signature", time.Now().Unix())
	assert.NoError(t, err)
	assert.True(t, valid)
}

// Test_RSAMessageSigner_Verify_EmptySignature 测试空签名的验证
func Test_RSAMessageSigner_Verify_EmptySignature(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSAMessageSigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 测试空签名验证
	hostname := "test-host"
	valid, err := signer.Verify(hostname, "", time.Now().Unix())
	assert.Error(t, err)
	assert.False(t, valid)
}
