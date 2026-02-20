package util

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

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

// Test_RSASigner_Sign_Correct 测试正确的签名生成
func Test_RSASigner_Sign_Correct(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)
	assert.True(t, signer.Enabled())

	// 测试签名生成
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	signature, err := signer.Sign(params)
	assert.NoError(t, err)
	assert.NotEmpty(t, signature)
}

// Test_RSASigner_Sign_Disabled 测试禁用签名时的行为
func Test_RSASigner_Sign_Disabled(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建禁用的签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, false)
	assert.NoError(t, err)
	assert.False(t, signer.Enabled())

	// 测试禁用时的签名生成
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	signature, err := signer.Sign(params)
	assert.NoError(t, err)
	assert.Empty(t, signature)
}

// Test_RSASigner_Sign_NoPrivateKey 测试无私钥时的行为
func Test_RSASigner_Sign_NoPrivateKey(t *testing.T) {
	// 生成测试密钥
	_, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys("", publicKeyPath)

	// 创建无私钥的签名器
	signer, err := NewRSASigner("", publicKeyPath, true)
	assert.NoError(t, err)
	assert.True(t, signer.Enabled())

	// 测试无私钥时的签名生成
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	signature, err := signer.Sign(params)
	assert.NoError(t, err)
	assert.Empty(t, signature)
}

// Test_RSASigner_Verify_Correct 测试正确的签名验证
func Test_RSASigner_Verify_Correct(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 生成签名
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	signature, err := signer.Sign(params)
	assert.NoError(t, err)
	assert.NotEmpty(t, signature)

	// 验证签名
	valid, err := signer.Verify(params, signature)
	assert.NoError(t, err)
	assert.True(t, valid)
}

// Test_RSASigner_Verify_Invalid 测试无效签名的验证
func Test_RSASigner_Verify_Invalid(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 测试无效签名
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}

	// 使用无效签名
	valid, err := signer.Verify(params, "invalid-signature")
	assert.Error(t, err)
	assert.False(t, valid)
}

// Test_RSASigner_Verify_WrongParams 测试错误参数的验证
func Test_RSASigner_Verify_WrongParams(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 生成签名
	originalParams := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	signature, err := signer.Sign(originalParams)
	assert.NoError(t, err)

	// 使用错误的参数验证
	wrongParams := map[string]interface{}{
		"hostname":  "wrong-host",
		"timestamp": 1234567890,
	}
	valid, err := signer.Verify(wrongParams, signature)
	assert.Error(t, err)
	assert.False(t, valid)
}

// Test_RSASigner_Verify_Disabled 测试禁用签名时的验证行为
func Test_RSASigner_Verify_Disabled(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建禁用的签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, false)
	assert.NoError(t, err)
	assert.False(t, signer.Enabled())

	// 测试禁用时的签名验证
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	valid, err := signer.Verify(params, "any-signature")
	assert.NoError(t, err)
	assert.True(t, valid)
}

// Test_RSASigner_Verify_NoPublicKey 测试无公钥时的验证行为
func Test_RSASigner_Verify_NoPublicKey(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, _, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, "")

	// 创建无公钥的签名器
	signer, err := NewRSASigner(privateKeyPath, "", true)
	assert.NoError(t, err)
	assert.True(t, signer.Enabled())

	// 测试无公钥时的签名验证
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	valid, err := signer.Verify(params, "any-signature")
	assert.NoError(t, err)
	assert.True(t, valid)
}

// Test_RSASigner_Verify_EmptySignature 测试空签名的验证
func Test_RSASigner_Verify_EmptySignature(t *testing.T) {
	// 生成测试密钥
	privateKeyPath, publicKeyPath, err := generateTestKeys()
	assert.NoError(t, err)
	defer cleanupTestKeys(privateKeyPath, publicKeyPath)

	// 创建签名器
	signer, err := NewRSASigner(privateKeyPath, publicKeyPath, true)
	assert.NoError(t, err)

	// 测试空签名验证
	params := map[string]interface{}{
		"hostname":  "test-host",
		"timestamp": 1234567890,
	}
	valid, err := signer.Verify(params, "")
	assert.Error(t, err)
	assert.False(t, valid)
}

// Test_buildSortedJSON 测试构建排序后的 JSON
func Test_buildSortedJSON(t *testing.T) {
	// 测试参数（key 无序）
	params := map[string]interface{}{
		"timestamp": 1234567890,
		"hostname":  "test-host",
		"status":    "online",
	}

	// 构建排序后的 JSON
	signContent, err := buildSortedJSON(params)
	assert.NoError(t, err)

	// 验证 JSON 是按 key 排序的
	expected := `{"hostname":"test-host","status":"online","timestamp":1234567890}`
	assert.Equal(t, expected, string(signContent))
}
