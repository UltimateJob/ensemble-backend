package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/domain"
)

// 连接器凭据的静态加密。
//
// 为什么值得做：connectors.json 里是飞书/Notion/GitHub 的 token。
// 数据目录经常被打包、同步、拷到另一台机器排查问题——明文 token
// 跟着走了一圈，用户是不知道的。
//
// 密钥从 JWT_SECRET 派生（生产自检强制它不能是默认值），放在环境变量
// 而不是数据目录里，意味着**单独拿到 data/ 或备份包解不开**。
//
// 边界要说清楚：这不防「整台机器被攻陷」——攻击者能读环境变量。
// 它防的是数据文件外流，那是实际发生过的那类事故。
//
// 注意：改 JWT_SECRET 会导致已存的凭据解不开（连接器显示需重新授权）。
// 这是刻意的取舍——密钥轮换本来就该配套重新授权。

const connAAD = "zhituan-connector-v1"

// connSealer 从 secret 派生 AES-256-GCM。
type connSealer struct {
	aead cipher.AEAD
}

func newConnSealer(secret string) (*connSealer, error) {
	if secret == "" {
		return nil, fmt.Errorf("缺少 JWT_SECRET，无法加密连接器凭据")
	}
	// 带域分隔的哈希派生，避免与 token 签名直接共用同一份密钥材料。
	sum := sha256.Sum256([]byte("zhituan/connector-secrets/v1\x00" + secret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &connSealer{aead: aead}, nil
}

// seal 把明文 map 加密成一个 base64 串（nonce 前置）。
func (s *connSealer) seal(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, raw, []byte(connAAD))
	return base64.StdEncoding.EncodeToString(out), nil
}

// open 解密 seal 的输出。
func (s *connSealer) open(blob string) (map[string]string, error) {
	if blob == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, err
	}
	ns := s.aead.NonceSize()
	if len(raw) < ns {
		return nil, fmt.Errorf("密文长度不足")
	}
	plain, err := s.aead.Open(nil, raw[:ns], raw[ns:], []byte(connAAD))
	if err != nil {
		// 密钥变了（JWT_SECRET 被改）或数据被篡改，都会到这里。
		return nil, fmt.Errorf("凭据解密失败（JWT_SECRET 是否改过？）：%w", err)
	}
	var m map[string]string
	if err := json.Unmarshal(plain, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// connectorRecord 是 connectors.json 里的存储形态。
// 与 domain.Connector 分开：落盘要加密，对外不能带密文。
type connectorRecord struct {
	ID         string            `json:"id"`
	UserID     string            `json:"userId"`
	Scope      string            `json:"scope"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	Config     map[string]string `json:"config,omitempty"`
	Sealed     string            `json:"sealed,omitempty"`
	SecretKeys []string          `json:"secretKeys,omitempty"`
	Status     string            `json:"status"`
	LastError  string            `json:"lastError,omitempty"`
	CheckedAt  time.Time         `json:"checkedAt"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

func connectorToRecord(c *domain.Connector) (connectorRecord, error) {
	sealed, err := sealer().seal(c.Secrets)
	if err != nil {
		return connectorRecord{}, err
	}
	keys := c.SecretKeys
	if len(keys) == 0 && len(c.Secrets) > 0 {
		keys = make([]string, 0, len(c.Secrets))
		for k := range c.Secrets {
			keys = append(keys, k)
		}
	}
	return connectorRecord{
		ID: c.ID, UserID: c.UserID, Scope: c.Scope, Kind: c.Kind, Name: c.Name,
		Enabled: c.Enabled, Config: c.Config, Sealed: sealed, SecretKeys: keys,
		Status: c.Status, LastError: c.LastError,
		CheckedAt: c.CheckedAt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}, nil
}

func connectorFromRecord(rec connectorRecord) *domain.Connector {
	secrets, err := sealer().open(rec.Sealed)
	if err != nil {
		// 单条解不开不该让整个服务起不来：把这条标成 error，
		// 用户重新填一次凭据即可，其余连接器照常工作。
		secrets = nil
		rec.Status = domain.ConnStatusError
		rec.LastError = err.Error()
	}
	return &domain.Connector{
		ID: rec.ID, UserID: rec.UserID, Scope: rec.Scope, Kind: rec.Kind,
		Name: rec.Name, Enabled: rec.Enabled, Config: rec.Config,
		Secrets: secrets, SecretKeys: rec.SecretKeys,
		Status: rec.Status, LastError: rec.LastError,
		CheckedAt: rec.CheckedAt, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
	}
}

// connectorStore 全局加密器。main 在启动时注入密钥。
//
// 用包级变量而不是给 FileStore 加字段：Store 接口已被多处实现
// （测试里的 Memory），多一个构造参数会波及所有调用点。
var connectorCrypto *connSealer

// SetSecretKey 注入连接器凭据的加密密钥（main 启动时调一次）。
func SetSecretKey(secret string) error {
	s, err := newConnSealer(secret)
	if err != nil {
		return err
	}
	connectorCrypto = s
	return nil
}

func sealer() *connSealer {
	if connectorCrypto == nil {
		// 没注入时用测试密钥兜底，保证单测能跑。
		// 生产路径由 main 保证先调 SetSecretKey。
		s, _ := newConnSealer("insecure-test-key")
		return s
	}
	return connectorCrypto
}

// fileSecure 把文件权限收紧到 0600。存凭据的文件不该对同机
// 其他用户可读——多用户机器上这是常见疏漏。
func fileSecure(path string) {
	_ = os.Chmod(path, 0o600)
}
