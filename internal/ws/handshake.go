package ws

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"

	agentcrypto "devops-agent/internal/crypto"
	"devops-agent/internal/protocol"
)

// BuildConnectRequest 构造 connect 请求帧，=
//
// 签名载荷为稳定串联字符串：
//
//	deviceId|nonce|signedAt|role|token
//
// 使用 UTF-8 字节序列通过 Ed25519 进行签名，结果以 Base64 编码写入 device.signature。
//
// 注意：该函数仅负责构造 JSON 结构，实际发送由 WebSocket 客户端实现。
func BuildConnectRequest(kp agentcrypto.KeyPair, authToken, deviceID, nonce string, minProtocol, maxProtocol int) (protocol.RequestFrame, error) {
	client := protocol.ClientInfo{
		ID:       "go-agent",
		Version:  "0.1.0-mvp",
		Platform: "linux-amd64", // TODO: 使用 runtime.GOOS + runtime.GOARCH
		Mode:     "node",
	}

	signedAt := time.Now().UnixMilli()

	devicePayload := protocol.DeviceInfo{
		ID:        deviceID,
		PublicKey: base64.StdEncoding.EncodeToString(kp.Public),
		Nonce:     nonce,
		SignedAt:  signedAt,
		// Signature 在后续填充
	}

	params := protocol.ConnectParams{
		MinProtocol: minProtocol,
		MaxProtocol: maxProtocol,
		Client:      client,
		Role:        "node",
		Scopes:      []string{}, // TODO: MVP 暂空，后续按权限模型填充
		Device:      devicePayload,
		Auth: protocol.AuthInfo{
			Token: authToken,
		},
	}

	// 构造签名载荷：
	// deviceId|nonce|signedAt|role|token
	payload := fmt.Sprintf("%s|%s|%d|%s|%s", devicePayload.ID, devicePayload.Nonce, devicePayload.SignedAt, params.Role, params.Auth.Token)

	sig := ed25519.Sign(kp.Private, []byte(payload))
	params.Device.Signature = base64.StdEncoding.EncodeToString(sig)

	return protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     uuid.NewString(), // 使用 UUID 作为请求 ID，便于与 correlationId 对齐。
		Method: protocol.MethodConnect,
		Params: params,
	}, nil
}
