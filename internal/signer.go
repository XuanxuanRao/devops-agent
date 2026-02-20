package internal

import (
	"time"

	"devops-agent/pkg/util"
)

// MessageSigner 消息签名接口
type MessageSigner interface {
	// Sign 对消息进行签名并返回签名和时间戳
	Sign(hostname string) (string, int64, error)

	// Verify 验证消息签名
	Verify(hostname string, signature string, timestamp int64) (bool, error)

	// Enabled 是否启用签名
	Enabled() bool
}

// RSAMessageSigner RSA 消息签名器
type RSAMessageSigner struct {
	signer util.Signer
}

// NewRSAMessageSigner 创建新的 RSA 签名器
func NewRSAMessageSigner(privateKeyPath, publicKeyPath string, enabled bool) (*RSAMessageSigner, error) {
	// 使用 util 包中的签名工具
	signer, err := util.NewRSASigner(privateKeyPath, publicKeyPath, enabled)
	if err != nil {
		return nil, err
	}

	return &RSAMessageSigner{
		signer: signer,
	}, nil
}

// Sign 对消息进行签名并返回签名和时间戳
func (s *RSAMessageSigner) Sign(hostname string) (string, int64, error) {
	// 检查是否启用签名
	if !s.signer.Enabled() {
		return "", 0, nil
	}

	// 生成时间戳
	timestamp := time.Now().Unix()

	// 构建签名参数
	params := map[string]interface{}{
		"hostname":  hostname,
		"timestamp": timestamp,
	}

	// 使用 util 包的签名方法
	signature, err := s.signer.Sign(params)
	if err != nil {
		return "", 0, err
	}

	// 如果签名为空（可能是因为没有私钥）
	if signature == "" {
		return "", 0, nil
	}

	return signature, timestamp, nil
}

// Verify 验证消息签名
func (s *RSAMessageSigner) Verify(hostname string, signature string, timestamp int64) (bool, error) {
	// 构建验证参数
	params := map[string]interface{}{
		"hostname":  hostname,
		"timestamp": timestamp,
	}

	// 使用 util 包的验证方法
	return s.signer.Verify(params, signature)
}

// Enabled 是否启用签名
func (s *RSAMessageSigner) Enabled() bool {
	return s.signer.Enabled()
}
