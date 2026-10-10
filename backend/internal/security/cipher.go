// Package security 提供凭证加密等安全能力。
//
// 只做一件事：把模型凭证加密后落库、解密后使用。
// 之所以自建而不引第三方：AES-256-GCM 是标准库直接支持的组合（对称加密 + 完整性校验），
// 自己写反而能把「nonce 从哪来、密钥从哪来、错了怎么办」写清楚。
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// KeySize 是 AES-256 要求的密钥长度（字节）。
const KeySize = 32

// 领域错误。
var (
	// ErrKeySize 表示密钥长度不合要求。
	ErrKeySize = errors.New("密钥长度必须是 32 字节")
	// ErrDecrypt 表示解密失败。可能原因：密钥换了、密文被改动、数据来自别的环境。
	// 刻意不区分具体原因：区分了反而给出可被利用的信息，且对排查没有帮助。
	ErrDecrypt = errors.New("凭证解密失败（密钥是否更换过？）")
	// ErrEmptyKey 表示未配置加密密钥。
	ErrEmptyKey = errors.New("未配置加密密钥")
)

// Cipher 用 AES-256-GCM 加解密凭证。
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher 用 32 字节密钥构造 Cipher。
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: 实际 %d 字节", ErrKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化 AES 失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 GCM 失败: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// ParseKey 解析配置里的密钥字符串。
//
// 支持三种写法，按「能唯一解析出 32 字节」的顺序尝试：
//  1. base64（推荐，openssl rand -base64 32 的输出）；
//  2. hex（64 个十六进制字符）；
//  3. 原始 32 字符。
//
// 容忍多种格式是为了不把「密钥格式不对」变成部署时的第一道坑；
// 但解析结果必须是 32 字节，凑不够或超长一律报错，不做补齐、不做截断。
func ParseKey(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, ErrEmptyKey
	}

	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	// base64url 变体（有些工具输出 - 和 _ 而不是 + 和 /）。
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	if len(value) == KeySize {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("%w: 解析后不是 32 字节（请用 `openssl rand -base64 32` 生成）", ErrKeySize)
}

// Encrypt 加密明文，返回 base64(nonce || 密文)。
//
// nonce 随机生成并**随密文一起保存**：GCM 的 nonce 绝不能用固定值，
// 同一密钥下重复 nonce 会直接破坏机密性（可恢复出明文异或关系）。
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	// 把 nonce 放在密文前面，解密时按同样长度切分。
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 Encrypt 的输出。
func (c *Cipher) Decrypt(encoded string) (string, error) {
	if strings.TrimSpace(encoded) == "" {
		// 空串是合法状态：表示该厂商还没填凭证。
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrDecrypt
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return "", ErrDecrypt
	}
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		// GCM 的校验失败就是「密文或密钥不对」，不要把它包装成通用错误，
		// 否则上层无法把「密钥换了」与「网络抖动」区分开。
		return "", ErrDecrypt
	}
	return string(plaintext), nil
}

// Mask 生成凭证的展示用掩码。
//
// 规则：长度 >= 8 时保留前 3 位与后 4 位，中间固定用 *** 代替；
// 更短的一律整体遮蔽。中间位数固定而不是保留原长度，是为了不泄漏密钥长度。
func Mask(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	runes := []rune(plaintext)
	if len(runes) < 8 {
		return "***"
	}
	return string(runes[:3]) + "***" + string(runes[len(runes)-4:])
}
