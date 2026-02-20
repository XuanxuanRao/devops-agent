package util

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Signer 签名工具接口
type Signer interface {
	// Sign 生成签名
	Sign(params map[string]interface{}) (string, error)

	// Verify 验证签名
	Verify(params map[string]interface{}, signature string) (bool, error)

	// Enabled 是否启用
	Enabled() bool
}

// RSASigner RSA 签名工具
type RSASigner struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	enabled    bool
}

// NewRSASigner 创建新的 RSA 签名工具
func NewRSASigner(privateKeyPath, publicKeyPath string, enabled bool) (*RSASigner, error) {
	signer := &RSASigner{
		enabled: enabled,
	}

	// 加载私钥（如果提供）
	if privateKeyPath != "" {
		privateKey, err := loadPrivateKey(privateKeyPath)
		if err != nil {
			log.Printf("Warning: Failed to load private key: %v", err)
		} else {
			signer.privateKey = privateKey
		}
	}

	// 加载公钥（如果提供）
	if publicKeyPath != "" {
		publicKey, err := loadPublicKey(publicKeyPath)
		if err != nil {
			log.Printf("Warning: Failed to load public key: %v", err)
		} else {
			signer.publicKey = publicKey
		}
	}

	return signer, nil
}

// Sign 生成签名
func (s *RSASigner) Sign(params map[string]interface{}) (string, error) {
	if !s.enabled || s.privateKey == nil {
		return "", nil
	}

	// 构建排序后的 JSON
	signContent, err := buildSortedJSON(params)
	if err != nil {
		return "", err
	}

	// 生成签名
	hash := sha256.Sum256(signContent)
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}

	// 编码签名
	signatureStr := base64.StdEncoding.EncodeToString(signature)

	return signatureStr, nil
}

// Verify 验证签名
func (s *RSASigner) Verify(params map[string]interface{}, signature string) (bool, error) {
	if !s.enabled || s.publicKey == nil {
		return true, nil
	}

	// 检查签名是否存在
	if signature == "" {
		return false, errors.New("missing signature")
	}

	// 构建排序后的 JSON
	signContent, err := buildSortedJSON(params)
	if err != nil {
		return false, err
	}

	// 解码签名
	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false, err
	}

	// 验证签名
	hash := sha256.Sum256(signContent)
	err = rsa.VerifyPKCS1v15(s.publicKey, crypto.SHA256, hash[:], signatureBytes)
	if err != nil {
		return false, err
	}

	return true, nil
}

// Enabled 是否启用
func (s *RSASigner) Enabled() bool {
	return s.enabled
}

// buildSortedJSON 构建排序后的 JSON
func buildSortedJSON(params map[string]interface{}) ([]byte, error) {
	// 获取所有 key 并排序
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// 构建排序后的 JSON
	var buf strings.Builder
	buf.WriteString("{")
	for i, key := range keys {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(`"`)
		buf.WriteString(key)
		buf.WriteString(`"`)
		buf.WriteString(":")

		// 序列化值
		value := params[key]
		switch v := value.(type) {
		case string:
			buf.WriteString(`"`)
			buf.WriteString(v)
			buf.WriteString(`"`)
		case int:
			buf.WriteString(strconv.Itoa(v))
		case int64:
			buf.WriteString(strconv.FormatInt(v, 10))
		case float64:
			buf.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		case bool:
			buf.WriteString(strconv.FormatBool(v))
		default:
			// 对于其他类型，使用 json.Marshal
			jsonValue, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			buf.WriteString(string(jsonValue))
		}
	}
	buf.WriteString("}")

	return []byte(buf.String()), nil
}

// loadPrivateKey 加载私钥（支持 PKCS#1 和 PKCS#8 格式）
func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private key format: no PEM block found")
	}

	var privateKey *rsa.PrivateKey

	// 尝试解析 PKCS#1 格式
	if block.Type == "RSA PRIVATE KEY" {
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err == nil {
			return privateKey, nil
		}
	}

	// 尝试解析 PKCS#8 格式
	if block.Type == "PRIVATE KEY" {
		parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			if rsaKey, ok := parsedKey.(*rsa.PrivateKey); ok {
				return rsaKey, nil
			}
			return nil, errors.New("invalid private key format: not an RSA key")
		}
	}

	return nil, errors.New("invalid private key format: unsupported type")
}

// loadPublicKey 加载公钥
func loadPublicKey(path string) (*rsa.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("invalid public key format")
	}

	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}

	return rsaPublicKey, nil
}
