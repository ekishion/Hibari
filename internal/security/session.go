package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// 会话令牌格式（不透明、无状态、自校验）：
//
//	v1.<base64url(payload)>.<base64url(hmac)>
//
// payload = 8字节签发时间 || 8字节过期时间 || 8字节随机 nonce（大端 Unix 秒）。
// 签名对 "v1.<base64url(payload)>" 计算 HMAC-SHA256，绑定版本前缀防止降级。
const (
	sessionTokenVersion = "v1"
	sessionPayloadLen   = 24 // iat(8) + exp(8) + nonce(8)
)

var sessionB64 = base64.RawURLEncoding

// SessionManager 负责签发与校验登录会话令牌。
type SessionManager struct {
	key []byte
	ttl time.Duration
}

func NewSessionManager(key []byte, ttl time.Duration) *SessionManager {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &SessionManager{key: key, ttl: ttl}
}

func (m *SessionManager) sign(signingInput string) []byte {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}

// Issue 依据当前时间签发一枚有效期为 m.ttl 的会话令牌。
func (m *SessionManager) Issue(now time.Time) (string, error) {
	payload := make([]byte, sessionPayloadLen)
	binary.BigEndian.PutUint64(payload[0:8], uint64(now.Unix()))
	binary.BigEndian.PutUint64(payload[8:16], uint64(now.Add(m.ttl).Unix()))
	if _, err := rand.Read(payload[16:24]); err != nil {
		return "", fmt.Errorf("生成会话随机数失败: %w", err)
	}
	signingInput := sessionTokenVersion + "." + sessionB64.EncodeToString(payload)
	sig := m.sign(signingInput)
	return signingInput + "." + sessionB64.EncodeToString(sig), nil
}

// Validate 校验令牌签名与有效期；任一环节失败均返回 false。
func (m *SessionManager) Validate(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != sessionTokenVersion {
		return false
	}
	payload, err := sessionB64.DecodeString(parts[1])
	if err != nil || len(payload) != sessionPayloadLen {
		return false
	}
	gotSig, err := sessionB64.DecodeString(parts[2])
	if err != nil {
		return false
	}
	signingInput := parts[0] + "." + parts[1]
	if !hmac.Equal(gotSig, m.sign(signingInput)) {
		return false
	}
	iat := int64(binary.BigEndian.Uint64(payload[0:8]))
	exp := int64(binary.BigEndian.Uint64(payload[8:16]))
	nowUnix := now.Unix()
	// 允许 60 秒时钟偏移，防止签发主机与校验主机轻微不同步导致刚签发即失效。
	if nowUnix+60 < iat {
		return false
	}
	return nowUnix < exp
}
