package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// 密码散列参数。格式固定下来是为了以后能平滑升级：
// pbkdf2-sha256$<迭代次数>$<salt-base64>$<hash-base64>
const (
	pbkdf2Prefix     = "pbkdf2-sha256"
	pbkdf2Iterations = 120000
	pbkdf2SaltLen    = 16
	pbkdf2KeyLen     = 32
	// pbkdf2MaxIterations 防坏数据：凭据文件里的迭代次数不能任由它写成天文数字，
	// 否则一次校验就能把 CPU 拖死。
	pbkdf2MaxIterations = 1000000
)

// hashPassword 生成带随机 salt 的密码散列。每次调用 salt 都不同，
// 同一个密码两次散列的结果也不同。
func hashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := pbkdf2SHA256([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLen)
	return fmt.Sprintf("%s$%d$%s$%s",
		pbkdf2Prefix,
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// verifyPassword 校验密码。任何格式错误都返回 false，绝不 panic——
// 凭据文件可能是旧版本或被人手改坏的。
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != pbkdf2Prefix {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 || iter > pbkdf2MaxIterations {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 || len(want) > 1024 {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2SHA256 PBKDF2-HMAC-SHA256（RFC 8018）。
// 标准库 Go 1.24 虽已提供 crypto/pbkdf2，这里手写一份，
// 保持实现自足且不依赖工具链细节。
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)
	t := make([]byte, hashLen)
	for block := 1; block <= numBlocks; block++ {
		// U1 = PRF(password, salt || INT_32_BE(block))
		prf.Reset()
		prf.Write(salt)
		prf.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u = prf.Sum(u[:0])
		copy(t, u)

		// U2..Uc 依次异或进 T
		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for i := range t {
				t[i] ^= u[i]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}
