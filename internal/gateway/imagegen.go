package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 生图渠道。
//
// 协议选 OpenAI 兼容的 /images/generations：多数中转站和自建网关都
// 提供这个形状，接一家等于接一片。国内厂商（通义/火山）请求体差异较大，
// 需要单独适配器；这里留了 Kind 让它们以后能作为另一种渠道类型加进来，
// 而不是把差异塞进同一个结构里。
//
// 请求形状：
//   POST {base}/images/generations
//   {"model":"gpt-image-1","prompt":"...","size":"1024x1024","n":1,
//    "response_format":"b64_json"}
//
// 响应有两条常见路径：
//   1. data[0].b64_json  —— 直接给图，省一次下载
//   2. data[0].url       —— 给临时链接，要再 GET 一次

// ImageRequest 一次生图请求。
type ImageRequest struct {
	Model  string `json:"model,omitempty"`
	Prompt string `json:"prompt"`
	Size   string `json:"size,omitempty"`
	N      int    `json:"n,omitempty"`
	// SavePath 工作区相对路径。空表示由调用方决定。
	SavePath string `json:"savePath,omitempty"`
}

// ImageResult 生图结果。
type ImageResult struct {
	// Files 落盘后的绝对路径（相对工作区解析前的）与大小。
	Files []ImageFile `json:"files"`
	Model string      `json:"model"`
	// RevisedPrompt 部分上游会把提示词改写，带回来便于排查
	RevisedPrompt string `json:"revisedPrompt,omitempty"`
}

// ImageFile 一张落盘的图。
type ImageFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ImageGen 一个生图渠道。
type ImageGen struct {
	name   string
	base   string
	key    string
	model  string
	client *http.Client
}

// NewImageGen 创建生图渠道。
func NewImageGen(name, base, key, model string, timeout time.Duration) *ImageGen {
	b := strings.TrimSuffix(strings.TrimSpace(base), "/")
	// 用户填的可能是 https://api.openai.com/v1，也可能是根地址。
	// 缺 /v1 时补上——和 Remote 的判断保持一致。
	if !strings.HasSuffix(b, "/v1") && !strings.Contains(b, "/v1/") {
		if !strings.Contains(b, "/openai") {
			b += "/v1"
		}
	}
	if name == "" {
		name = "image"
	}
	return &ImageGen{
		name:  name,
		base:  b,
		key:   key,
		model: model,
		client: &http.Client{
			// 生图比对话慢得多：DALL·E 3 要 30-60 秒，中转站波动更大。
			// 这里给足 180 秒，但用 Transport 控制「等响应头」而不是整体超时，
			// 因为下载图片正文的时间不该被算在这里。
			Transport: &http.Transport{
				MaxIdleConns:          20,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
				ResponseHeaderTimeout: 180 * time.Second,
				ForceAttemptHTTP2:     true,
			},
			Timeout: timeout,
		},
	}
}

func (g *ImageGen) Name() string { return g.name }

// Generate 生成图片并写到 outDir。
//
// 返回的 Path 是写盘后的绝对路径；调用方负责校验它落在工作区内。
func (g *ImageGen) Generate(ctx context.Context, req ImageRequest, outDir string) (*ImageResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("提示词不能为空")
	}
	model := req.Model
	if model == "" {
		model = g.model
	}
	n := req.N
	if n <= 0 {
		n = 1
	}
	if n > 4 {
		n = 4 // 多数上游单次最多 4 张，超过会被拒
	}
	size := req.Size
	if size == "" {
		size = "1024x1024"
	}

	body := map[string]any{
		"model":  model,
		"prompt": req.Prompt,
		"n":      n,
		"size":   size,
		// 要求 base64：省一次对外 GET，也让结果不依赖临时链接的有效期
		"response_format": "b64_json",
	}
	raw, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.base+"/images/generations", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.key)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("image: %w", err)
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode != http.StatusOK {
		snippet := strings.TrimSpace(string(payload))
		if len([]rune(snippet)) > 300 {
			snippet = string([]rune(snippet)[:300]) + "…"
		}
		return nil, fmt.Errorf("image: %d %s", resp.StatusCode, snippet)
	}

	var out struct {
		Data []struct {
			B64JSON       string `json:"b64_json"`
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("image: 上游返回了无法解析的内容")
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, errors.New(out.Error.Message)
	}
	if len(out.Data) == 0 {
		return nil, errors.New("image: 上游没有返回图片")
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	res := &ImageResult{Model: model}
	for i, d := range out.Data {
		var data []byte
		switch {
		case d.B64JSON != "":
			data, err = base64.StdEncoding.DecodeString(d.B64JSON)
			if err != nil {
				return nil, fmt.Errorf("image: 图片解码失败 %v", err)
			}
		case d.URL != "":
			data, err = g.download(ctx, d.URL)
			if err != nil {
				return nil, err
			}
		default:
			continue
		}
		name := fmt.Sprintf("img-%s-%d.png", time.Now().Format("20060102-150405"), i+1)
		p := filepath.Join(outDir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return nil, fmt.Errorf("image: 写入失败 %v", err)
		}
		res.Files = append(res.Files, ImageFile{Path: p, Size: int64(len(data))})
		if res.RevisedPrompt == "" {
			res.RevisedPrompt = d.RevisedPrompt
		}
	}
	if len(res.Files) == 0 {
		return nil, errors.New("image: 没有可用的图片数据")
	}
	return res, nil
}

// download 取回 url 指向的图片。中转站常用短时签名链接，
// 不带 Authorization——带上反而可能被对象存储拒绝。
func (g *ImageGen) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("image: 下载失败 %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image: 下载返回 %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// Probe 探一次渠道是否可用。生图没有免费的「列模型」端点，
// 只能发一个最小的真实请求——这会花钱，所以默认不在健康检查里跑。
func (g *ImageGen) Probe(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "imgprobe")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	_, err = g.Generate(ctx, ImageRequest{Prompt: "a single red dot", Size: "256x256", N: 1}, dir)
	return err
}
