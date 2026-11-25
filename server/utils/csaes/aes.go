// Package csaes 客服系统 SSO 渠道参数加解密(AES-256-GCM)
// 约定: key = SHA256(配置密钥字符串); 密文 = base64( iv[12B] || ciphertext||tag )
// GCM 自带完整性认证, 密钥错误/篡改一律解密失败, 无需额外签名位
package csaes

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var (
	ErrCipherFormat = errors.New("密文格式非法")
	ErrDecryptFail  = errors.New("解密失败, 密文无效或密钥不匹配")
)

func newGCM(secret string) (cipher.AEAD, error) {
	sum := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt 加密明文(商户联调/测试构造链接用)
func Encrypt(secret string, plain []byte) (string, error) {
	gcm, err := newGCM(secret)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize()) // 12B
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(iv, iv, plain, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 base64 密文, 返回明文
func Decrypt(secret, b64Cipher string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64Cipher)
	if err != nil {
		return nil, ErrCipherFormat
	}
	gcm, err := newGCM(secret)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, ErrCipherFormat
	}
	iv, sealed := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return nil, ErrDecryptFail
	}
	return plain, nil
}
