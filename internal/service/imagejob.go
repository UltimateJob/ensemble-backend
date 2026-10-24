package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
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
	RevisedPrompt string `json:"revisedPrompt,omitempty"`
	Provider      string `json:"provider,omitempty"`
	// Edit 这次任务是在改图（图生图）而不是从零生成。
	Edit      bool      `json:"edit,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	EndedAt   time.Time `json:"endedAt,omitempty"`

	cancel context.CancelFunc
}

// ImageJobs 任务表。同一个进程可能同时跑几个生图，用 mutex 保护。
type ImageJobs struct {
	mu    sync.Mutex
	rows  map[string]*ImageJob
	order []string // 保留插入顺序，列表时新任务在前
	gens  []*gateway.ImageGen
	// rr 轮询游标。多个生图渠道时轮流用，而不是全压在一个上——
	// 中转站的限流和额度是按 key 算的，摊开能多撑一会儿。
	rr     int
	muRR   sync.Mutex
	outDir string
	// maxKeep 最多保留多少条已完成任务，防止内存无限增长
	maxKeep int
}

// NewImageJobs 创建任务表。outDir 是图片落盘目录。
// gens 为空表示没有配置生图渠道。
func NewImageJobs(gens []*gateway.ImageGen, outDir string) *ImageJobs {
	return &ImageJobs{
		rows:    make(map[string]*ImageJob),
		gens:    gens,
		outDir:  outDir,
		maxKeep: 200,
	}
}

// pickGen 按风格模式选一个渠道。
//
// pref 是用户点名的渠道名（固定风格）；为空表示随机风格——
// 这时按轮询而不是纯随机分配：纯随机在小样本下会连续抽中同一个，
// 摊不开限流，也容易让用户觉得「随机」其实没生效。
func (j *ImageJobs) pickGen(pref string) *gateway.ImageGen {
	if len(j.gens) == 0 {
		return nil
	}
	if pref != "" {
		for _, g := range j.gens {
			if g.Name() == pref {
				return g
			}
		}
		// 点名的渠道已经没了（运维台删了 / 停用了）：
		// 退回轮询，总比让用户拿到一个「渠道不存在」好。
	}
	j.muRR.Lock()
	defer j.muRR.Unlock()
	g := j.gens[j.rr%len(j.gens)]
	j.rr++
	return g
}

// Gen 按名字查渠道，供「固定风格」列表展示模型名。
func (j *ImageJobs) Gen(name string) *gateway.ImageGen {
	for _, g := range j.gens {
		if g.Name() == name {
			return g
		}
	}
	return nil
}

// Providers 列出当前可用的生图渠道名，供界面展示与排障。
func (j *ImageJobs) Providers() []string {
	if j == nil {
		return nil
	}
	out := make([]string, 0, len(j.gens))
	for _, g := range j.gens {
		out = append(out, g.Name())
	}
	return out
}

var (
	ErrImageDisabled = errors.New("没有可用的生图渠道，请先在运维台添加")
	ErrJobNotFound   = errors.New("生图任务不存在")
)

// Enabled 是否配了生图渠道。
func (j *ImageJobs) Enabled() bool { return j != nil && len(j.gens) > 0 }

// Submit 提交一个任务并立即返回 id。真正的生成在后台 goroutine 里跑。
func (j *ImageJobs) Submit(userID string, req gateway.ImageRequest) (*ImageJob, error) {
	if !j.Enabled() {
		return nil, ErrImageDisabled
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	gen := j.pickGen(req.Provider)
	if gen == nil {
		cancel()
		return nil, ErrImageDisabled
	}

	job := &ImageJob{
		ID:        store.NewID("img"),
		UserID:    userID,
		Prompt:    req.Prompt,
		Edit:      len(req.SourceImage) > 0,
		Size:      req.Size,
		N:         req.N,
		Model:     req.Model,
		Status:    "pending",
		Provider:  gen.Name(),
		CreatedAt: time.Now(),
		cancel:    cancel,
	}

	j.mu.Lock()
	j.rows[job.ID] = job
	j.order = append(j.order, job.ID)
	j.trimLocked()
	j.mu.Unlock()

	go j.run(ctx, job, req, gen)
	return job, nil
}

func (j *ImageJobs) run(ctx context.Context, job *ImageJob, req gateway.ImageRequest, gen *gateway.ImageGen) {
	j.mu.Lock()
	job.Status = "running"
	j.mu.Unlock()

	var (
		res *gateway.ImageResult
		err error
	)
	if len(req.SourceImage) > 0 {
		res, err = gen.Edit(ctx, req, j.outDir)
	} else {
		res, err = gen.Generate(ctx, req, j.outDir)
	}

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

// File 返回任务产出的第 i 张图。只有创建者能取。
//
// 按任务 + 序号取而不是让调用方传路径：后者等于开了一个能读服务器
// 任意文件的接口。这里只认自己生成过、且记在任务里的那些文件。
func (j *ImageJobs) File(userID, id string, i int) (string, error) {
	j.mu.Lock()
	job, ok := j.rows[id]
	j.mu.Unlock()
	if !ok || job.UserID != userID {
		return "", ErrJobNotFound
	}
	if i < 0 || i >= len(job.Files) {
		return "", errors.New("没有这张图")
	}
	p := job.Files[i].Path
	// 再确认它确实落在输出目录里：任务表是内存态，
	// 但路径最终来自上游响应，不该无条件信任。
	if !strings.HasPrefix(filepath.Clean(p), filepath.Clean(j.outDir)+string(filepath.Separator)) {
		return "", errors.New("文件路径不合法")
	}
	return p, nil
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
