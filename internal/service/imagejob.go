package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/UltimateJob/ensemble-backend/internal/gateway"
	"github.com/UltimateJob/ensemble-backend/internal/store"
)

// 生图任务。
//
// 为什么是异步：一张图 10-60 秒，DALL·E 3 更慢，中转站波动更大。
// 同步接口会把 HTTP 连接挂在那里，前端只能转圈，Agent 也卡在一个
// 工具调用里不能做别的。做成任务后：
//   - 提交立即返回 job id
//   - 前台轮询 /v1/image-jobs/{id}，完成时拿到文件路径
//   - Agent 通过工具提交，之后可以继续干别的，到点再看
//
// 任务只在内存里：进程重启时未完成的任务就没了。这是有意的——
// 重跑到一半的生图任务恢复起来要处理上游账单，比让用户重发一次复杂。

// ImageJob 一次生图任务。
type ImageJob struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Prompt string `json:"prompt"`
	Size   string `json:"size,omitempty"`
	N      int    `json:"n,omitempty"`
	Model  string `json:"model,omitempty"`
	// Status pending | running | done | failed
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// Files 完成后落盘的图片（绝对路径 + 大小）
	Files []gateway.ImageFile `json:"files,omitempty"`
	// RevisedPrompt 上游改写后的提示词
	RevisedPrompt string    `json:"revisedPrompt,omitempty"`
	Provider      string    `json:"provider,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	EndedAt       time.Time `json:"endedAt,omitempty"`

	cancel context.CancelFunc
}

// ImageJobs 任务表。同一个进程可能同时跑几个生图，用 mutex 保护。
type ImageJobs struct {
	mu     sync.Mutex
	rows   map[string]*ImageJob
	order  []string // 保留插入顺序，列表时新任务在前
	gen    *gateway.ImageGen
	outDir string
	// maxKeep 最多保留多少条已完成任务，防止内存无限增长
	maxKeep int
}

// NewImageJobs 创建任务表。outDir 是图片落盘目录。
func NewImageJobs(gen *gateway.ImageGen, outDir string) *ImageJobs {
	return &ImageJobs{
		rows:    make(map[string]*ImageJob),
		gen:     gen,
		outDir:  outDir,
		maxKeep: 200,
	}
}

var (
	ErrImageDisabled = errors.New("没有可用的生图渠道，请先在运维台添加")
	ErrJobNotFound   = errors.New("生图任务不存在")
)

// Enabled 是否配了生图渠道。
func (j *ImageJobs) Enabled() bool { return j != nil && j.gen != nil }

// Submit 提交一个任务并立即返回 id。真正的生成在后台 goroutine 里跑。
func (j *ImageJobs) Submit(userID string, req gateway.ImageRequest) (*ImageJob, error) {
	if !j.Enabled() {
		return nil, ErrImageDisabled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	job := &ImageJob{
		ID:        store.NewID("img"),
		UserID:    userID,
		Prompt:    req.Prompt,
		Size:      req.Size,
		N:         req.N,
		Model:     req.Model,
		Status:    "pending",
		Provider:  j.gen.Name(),
		CreatedAt: time.Now(),
		cancel:    cancel,
	}

	j.mu.Lock()
	j.rows[job.ID] = job
	j.order = append(j.order, job.ID)
	j.trimLocked()
	j.mu.Unlock()

	go j.run(ctx, job, req)
	return job, nil
}

func (j *ImageJobs) run(ctx context.Context, job *ImageJob, req gateway.ImageRequest) {
	j.mu.Lock()
	job.Status = "running"
	j.mu.Unlock()

	res, err := j.gen.Generate(ctx, req, j.outDir)

	j.mu.Lock()
	defer j.mu.Unlock()
	job.EndedAt = time.Now()
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		log.Printf("[image] 任务 %s 失败：%v", job.ID, err)
		return
	}
	job.Status = "done"
	job.Files = res.Files
	job.RevisedPrompt = res.RevisedPrompt
	if job.Model == "" {
		job.Model = res.Model
	}
}

// Get 取一个任务。只有创建者能看。
func (j *ImageJobs) Get(userID, id string) (*ImageJob, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	job, ok := j.rows[id]
	if !ok || job.UserID != userID {
		return nil, ErrJobNotFound
	}
	return copyJob(job), nil
}

// List 列出该用户的最近任务，新的在前。
func (j *ImageJobs) List(userID string, limit int) []*ImageJob {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []*ImageJob
	for i := len(j.order) - 1; i >= 0 && len(out) < limit; i-- {
		if job, ok := j.rows[j.order[i]]; ok && job.UserID == userID {
			out = append(out, copyJob(job))
		}
	}
	return out
}

// Cancel 中止一个还在跑的任务。
func (j *ImageJobs) Cancel(userID, id string) error {
	j.mu.Lock()
	job, ok := j.rows[id]
	if !ok || job.UserID != userID {
		j.mu.Unlock()
		return ErrJobNotFound
	}
	cancel := job.cancel
	j.mu.Unlock()
	if cancel != nil && (job.Status == "pending" || job.Status == "running") {
		cancel()
	}
	return nil
}

// trimLocked 丢弃最旧的已完成任务。调用方持有锁。
func (j *ImageJobs) trimLocked() {
	if len(j.order) <= j.maxKeep {
		return
	}
	drop := len(j.order) - j.maxKeep
	kept := make([]string, 0, len(j.order))
	for i, id := range j.order {
		if i < drop {
			if job, ok := j.rows[id]; ok && (job.Status == "pending" || job.Status == "running") {
				kept = append(kept, id) // 在跑的不丢
				continue
			}
			delete(j.rows, id)
			continue
		}
		kept = append(kept, id)
	}
	j.order = kept
}

func copyJob(j *ImageJob) *ImageJob {
	c := *j
	c.cancel = nil
	return &c
}

// ImagePrompt 给 Agent 看的能力说明。放在这里是因为它和渠道是否可用强相关。
const ImagePrompt = "生成图片。返回任务 id，图片稍后落盘到工作区。"

// MarshalJob 便于 SSE / HTTP 输出。
func MarshalJob(j *ImageJob) json.RawMessage {
	b, _ := json.Marshal(j)
	return b
}

var _ = os.Remove
var _ = filepath.Join
