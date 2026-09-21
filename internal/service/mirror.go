package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"time"

	"AVDDesktop/internal/domain"
	"AVDDesktop/internal/job"
	"AVDDesktop/internal/mirror"
)

// MirrorService 提供镜像源管理与测速。
type MirrorService struct{ rt *Runtime }

// NewMirrorService 创建 MirrorService。
func NewMirrorService(rt *Runtime) *MirrorService { return &MirrorService{rt: rt} }

// ListSources 返回所有镜像源（内置 + 自定义），并附带缓存的测速结果。
func (s *MirrorService) ListSources() []domain.MirrorSource {
	settings := s.rt.settings.Get()
	sources := mirror.Merge(settings.CustomSources)
	cached := map[string]domain.SpeedResult{}
	for _, r := range s.rt.engine.Cached() {
		cached[r.SourceID] = r
	}
	for i := range sources {
		if r, ok := cached[sources[i].ID]; ok {
			rr := r
			sources[i].LastResult = &rr
			sources[i].Grade = mirror.GradeFromResult(r)
		}
	}
	return sources
}

// AddSourceRequest 是新增自定义源的请求。
type AddSourceRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	Probe   bool   `json:"probe"`
}

// AddSource 添加自定义镜像源（可选择立即预检）。
func (s *MirrorService) AddSource(req AddSourceRequest) (*domain.MirrorSource, error) {
	base, err := mirror.NormalizeBaseURL(req.BaseURL)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = base
	}
	src := domain.MirrorSource{
		ID:      customSourceID(base),
		Name:    name,
		BaseURL: base,
		Kind:    domain.MirrorCustom,
		Enabled: true,
		Note:    "用户自定义源",
	}
	if req.Probe {
		res := s.rt.engine.Test(s.rt.Context(), src, mirror.TestOptions{}.WithDefaults())
		src.Grade = mirror.GradeFromResult(res)
		src.LastResult = &res
	}
	if _, err := s.rt.settings.AddCustomSource(src); err != nil {
		return nil, err
	}
	return &src, nil
}

// UpdateSourceRequest 是修改自定义源的请求。
type UpdateSourceRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseURL"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// UpdateSource 修改自定义源。
func (s *MirrorService) UpdateSource(req UpdateSourceRequest) (*domain.MirrorSource, error) {
	settings := s.rt.settings.Get()
	current, ok := mirror.Find(settings.CustomSources, req.ID)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "只能修改自定义源: "+req.ID)
	}
	if strings.TrimSpace(req.Name) != "" {
		current.Name = strings.TrimSpace(req.Name)
	}
	if strings.TrimSpace(req.BaseURL) != "" {
		base, err := mirror.NormalizeBaseURL(req.BaseURL)
		if err != nil {
			return nil, err
		}
		current.BaseURL = base
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
	}
	if _, err := s.rt.settings.AddCustomSource(current); err != nil {
		return nil, err
	}
	return &current, nil
}

// RemoveSource 删除自定义源。
func (s *MirrorService) RemoveSource(id string) error {
	settings := s.rt.settings.Get()
	if _, ok := mirror.Find(settings.CustomSources, id); !ok {
		return domain.Err(domain.CodeInvalidArgument, "内置镜像源不能删除，只可停用: "+id)
	}
	_, err := s.rt.settings.RemoveCustomSource(id)
	return err
}

// SetActiveSource 设置当前使用的镜像源。
func (s *MirrorService) SetActiveSource(id string) error {
	sources := mirror.Merge(s.rt.settings.Get().CustomSources)
	if _, ok := mirror.Find(sources, id); !ok {
		return domain.Err(domain.CodeInvalidArgument, "镜像源不存在: "+id)
	}
	_, err := s.rt.settings.Update(map[string]any{"activeSourceId": id})
	return err
}

// TestRequest 是测速请求。
type TestRequest struct {
	SourceIDs       []string `json:"sourceIds"`
	MaxThroughputMB int      `json:"maxThroughputMB"`
	Quick           bool     `json:"quick"`
}

// TestSources 并发测速所有（或指定）源，结果通过事件流式推送，返回 jobID。
func (s *MirrorService) TestSources(req TestRequest) (string, error) {
	sources := s.ListSources()
	if len(req.SourceIDs) > 0 {
		filtered := make([]domain.MirrorSource, 0, len(req.SourceIDs))
		for _, id := range req.SourceIDs {
			if src, ok := mirror.Find(sources, id); ok {
				filtered = append(filtered, src)
			}
		}
		sources = filtered
	}
	if len(sources) == 0 {
		return "", domain.Err(domain.CodeInvalidArgument, "没有可测试的镜像源")
	}

	opts := mirror.TestOptions{}
	if req.MaxThroughputMB > 0 {
		opts.MaxThroughputBytes = int64(req.MaxThroughputMB) << 20
	}
	opts.SkipThroughput = req.Quick
	opts = opts.WithDefaults()

	timeout := opts.Timeout*time.Duration(len(sources)) + 30*time.Second

	j := s.rt.jobs.Start(s.rt.Context(), job.Spec{
		Kind:       domain.JobSpeedTest,
		Title:      "镜像源测速",
		Subtitle:   "共 " + itoa(len(sources)) + " 个源",
		ItemsTotal: len(sources),
	}, func(ctx context.Context, j *job.Job) error {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		done := 0
		j.SetPhase("正在测速…")
		s.rt.engine.TestAll(ctx, sources, opts, func(r domain.SpeedResult) {
			done++
			j.SetItems(done, len(sources))
			j.SetPercent(float64(done) / float64(len(sources)) * 100)
			j.Logf(levelFor(r), "speedtest", "%s: %s 延迟 %dms 速度 %.2fMB/s 评分 %d",
				r.SourceID, r.Grade, r.TTFBMs, r.ThroughputMBps, r.Score)
			s.rt.Emit("speedtest:result", r)
		})
		return nil
	})
	return j.ID(), nil
}

// ProbeSource 同步快速预检单个源（添加自定义源时使用）。
func (s *MirrorService) ProbeSource(id string) (*domain.SpeedResult, error) {
	sources := s.ListSources()
	src, ok := mirror.Find(sources, id)
	if !ok {
		return nil, domain.Err(domain.CodeInvalidArgument, "镜像源不存在: "+id)
	}
	opts := mirror.TestOptions{SkipThroughput: true}.WithDefaults()
	opts.Timeout = 20 * time.Second
	res := s.rt.engine.Test(s.rt.Context(), src, opts)
	return &res, nil
}

// GetCachedResults 返回缓存的测速结果。
func (s *MirrorService) GetCachedResults() []domain.SpeedResult {
	return s.rt.engine.Cached()
}

// CancelTest 取消测速任务。
func (s *MirrorService) CancelTest(jobID string) error {
	return s.rt.jobs.Cancel(jobID)
}

// TestAllQuick 对全部源做一次"仅索引"的快速测速（首页展示用）。
func (s *MirrorService) TestAllQuick() (string, error) {
	return s.TestSources(TestRequest{Quick: true})
}

func customSourceID(base string) string {
	sum := sha1.Sum([]byte(base))
	return "custom-" + hex.EncodeToString(sum[:])[:12]
}

func levelFor(r domain.SpeedResult) string {
	switch r.Grade {
	case "recommended", "usable":
		return "info"
	default:
		return "warn"
	}
}
