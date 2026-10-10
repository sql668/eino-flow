package security_test

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"einoflow-backend/internal/security"
)

// testKey 是测试用的固定密钥（32 字节），不涉及任何真实凭证。
var testKey = []byte("0123456789abcdef0123456789abcdef")

func newCipher(t *testing.T) *security.Cipher {
	t.Helper()
	cipher, err := security.NewCipher(testKey)
	if err != nil {
		t.Fatalf("构造 Cipher 失败: %v", err)
	}
	return cipher
}

// TestCipher_RoundTrip 覆盖加解密往返，并确认密文里看不到明文。
func TestCipher_RoundTrip(t *testing.T) {
	cipher := newCipher(t)
	secret := "sk-abcdefghijklmnopqrstuvwxyz123456"

	encrypted, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if strings.Contains(encrypted, secret) {
		t.Fatal("密文里出现了明文")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if decrypted != secret {
		t.Fatalf("解密结果 = %q, 期望 %q", decrypted, secret)
	}

	// 同样的明文必须得到不同密文：nonce 每次随机，否则相同 key 的厂商会在库里长得一样。
	again, err := cipher.Encrypt(secret)
	if err != nil {
		t.Fatalf("二次加密失败: %v", err)
	}
	if again == encrypted {
		t.Error("两次加密结果相同，nonce 可能未随机")
	}
}

// TestCipher_EmptyPlaintext 覆盖空凭证：这是「尚未填写」的正常状态，不是错误。
func TestCipher_EmptyPlaintext(t *testing.T) {
	cipher := newCipher(t)
	encrypted, err := cipher.Encrypt("")
	if err != nil {
		t.Fatalf("加密空串失败: %v", err)
	}
	if encrypted == "" {
		t.Fatal("空串加密后不应还是空串（否则无法与「未加密」区分）")
	}
	decrypted, err := cipher.Decrypt("")
	if err != nil || decrypted != "" {
		t.Fatalf("解密空串 = %q, %v；期望空串且无错误", decrypted, err)
	}
}

// TestCipher_WrongKey 覆盖换密钥后的行为：必须报 ErrDecrypt 而不是返回乱码。
func TestCipher_WrongKey(t *testing.T) {
	encrypted, err := newCipher(t).Encrypt("sk-secret-value")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	other, err := security.NewCipher([]byte("ffffffffffffffffffffffffffffffff"))
	if err != nil {
		t.Fatalf("构造第二个 Cipher 失败: %v", err)
	}
	if _, err := other.Decrypt(encrypted); !errors.Is(err, security.ErrDecrypt) {
		t.Fatalf("错误 = %v, 期望 ErrDecrypt", err)
	}
}

// TestCipher_Tampered 覆盖密文被改动：GCM 的完整性校验必须拦下。
func TestCipher_Tampered(t *testing.T) {
	cipher := newCipher(t)
	encrypted, err := cipher.Encrypt("sk-secret-value")
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("解码密文失败: %v", err)
	}
	raw[len(raw)-1] ^= 0x01 // 翻转最后一比特

	if _, err := cipher.Decrypt(base64.StdEncoding.EncodeToString(raw)); !errors.Is(err, security.ErrDecrypt) {
		t.Fatalf("错误 = %v, 期望 ErrDecrypt", err)
	}
	if _, err := cipher.Decrypt("这不是 base64"); !errors.Is(err, security.ErrDecrypt) {
		t.Fatalf("非法输入错误 = %v, 期望 ErrDecrypt", err)
	}
	if _, err := cipher.Decrypt("YWJj"); !errors.Is(err, security.ErrDecrypt) {
		t.Fatalf("过短密文错误 = %v, 期望 ErrDecrypt", err)
	}
}

// TestNewCipher_RejectsWrongKeySize 覆盖密钥长度校验。
func TestNewCipher_RejectsWrongKeySize(t *testing.T) {
	for _, key := range [][]byte{nil, {}, []byte("short"), []byte(strings.Repeat("a", 31)), []byte(strings.Repeat("a", 33))} {
		if _, err := security.NewCipher(key); !errors.Is(err, security.ErrKeySize) {
			t.Errorf("密钥长度 %d 的错误 = %v, 期望 ErrKeySize", len(key), err)
		}
	}
}

// TestParseKey 覆盖三种密钥写法与错误输入。
func TestParseKey(t *testing.T) {
	raw := strings.Repeat("k", 32)
	cases := map[string]struct {
		input   string
		want    []byte
		wantErr error
	}{
		"base64":    {base64.StdEncoding.EncodeToString([]byte(raw)), []byte(raw), nil},
		"base64url": {base64.RawURLEncoding.EncodeToString([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8, 0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0, 0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8, 0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0}), nil, nil},
		"hex":       {hex.EncodeToString([]byte(raw)), []byte(raw), nil},
		"raw32":     {raw, []byte(raw), nil},
		"带空白":       {"  " + raw + "\n", []byte(raw), nil},
		"空串":        {"", nil, security.ErrEmptyKey},
		"太短":        {"abc", nil, security.ErrKeySize},
		"太长的明文短串":   {strings.Repeat("x", 64), nil, security.ErrKeySize},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := security.ParseKey(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("错误 = %v, 期望 %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if tc.want != nil && string(got) != string(tc.want) {
				t.Fatalf("解析结果 = %q, 期望 %q", got, tc.want)
			}
		})
	}
}

// TestMask 覆盖掩码规则：永远不出现完整凭证，且不泄漏长度。
func TestMask(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"abc":                          "***",
		"1234567":                      "***",
		"sk-1234567890abcdef":          "sk-***cdef",
		"0123456789abcdef0123456789ab": "012***89ab",
	}
	for input, want := range cases {
		if got := security.Mask(input); got != want {
			t.Errorf("Mask(%q) = %q, 期望 %q", input, got, want)
		}
	}
	// 掩码结果绝不能等于原值。
	long := "sk-verysecretvalue12345"
	if security.Mask(long) == long {
		t.Error("掩码结果与原值相同")
	}
}
