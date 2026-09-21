package job

import (
	"context"
	"testing"
	"time"

	"AVDDesktop/internal/domain"
)

// TestJobLogLinesCarryUniqueSeq 验证同一任务内的每条日志都有唯一且递增的序号。
//
// 前端控制台用 seq 作为行 id 去重（实时推送与历史回填携带同一序号）。
// 任务日志若没有序号，只能退化成「时间 + 来源 + 文本」做 id，
// 同一毫秒内的相同文本会被判成重复行而丢弃——进度类日志极易撞在一起，
// 表现出来就是「日志不实时更新」。
func TestJobLogLinesCarryUniqueSeq(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Stop()

	j := m.Start(context.Background(), Spec{Kind: domain.JobInstall, Title: "安装组件"},
		func(ctx context.Context, j *Job) error {
			for i := 0; i < 5; i++ {
				j.Log("info", "sdkmanager", "同一行文本") // 文本刻意完全相同
			}
			return nil
		})

	waitJobEnded(t, j)

	logs := j.Logs()
	if len(logs) != 5 {
		t.Fatalf("任务日志 %d 条，期望 5 条", len(logs))
	}
	var prev uint64
	for i, line := range logs {
		if line.Seq == 0 {
			t.Fatalf("第 %d 条日志缺少序号：%+v", i+1, line)
		}
		if line.Seq <= prev {
			t.Fatalf("序号未按写入顺序递增：第 %d 条 seq=%d，上一条 seq=%d", i+1, line.Seq, prev)
		}
		prev = line.Seq
	}
}

// TestJobLogSeqMatchesPushedLines 验证实时推送与历史回填携带完全相同的序号。
//
// 这是前端去重的正确性前提：同一行无论是推送来的还是回填来的，lineId 必须一致。
func TestJobLogSeqMatchesPushedLines(t *testing.T) {
	pushed := make(chan []domain.LogLine, 8)
	m := NewManager(func(event string, payload any) {
		if event != EventLog {
			return
		}
		batch, ok := payload.(map[string]any)
		if !ok {
			return
		}
		if lines, ok := batch["lines"].([]domain.LogLine); ok {
			pushed <- lines
		}
	}, nil)
	defer m.Stop()

	j := m.Start(context.Background(), Spec{Kind: domain.JobInstall, Title: "安装组件"},
		func(ctx context.Context, j *Job) error {
			j.Log("info", "sdkmanager", "a")
			j.Log("info", "sdkmanager", "b")
			return nil
		})

	var streamed []domain.LogLine
	deadline := time.After(2 * time.Second)
	for len(streamed) < 2 {
		select {
		case batch := <-pushed:
			streamed = append(streamed, batch...)
		case <-deadline:
			t.Fatalf("2s 内只收到 %d 条推送日志，期望 2 条", len(streamed))
		}
	}

	backfill := j.Logs()
	if len(backfill) != len(streamed) {
		t.Fatalf("回填 %d 条，推送 %d 条", len(backfill), len(streamed))
	}
	for i := range backfill {
		if backfill[i].Seq != streamed[i].Seq {
			t.Errorf("第 %d 条：回填 seq=%d，推送 seq=%d（同一行必须同 id，否则会被重复插入或误删）",
				i+1, backfill[i].Seq, streamed[i].Seq)
		}
	}
}

func waitJobEnded(t *testing.T, j *Job) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		switch j.Info().Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCanceled:
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("任务未在 3s 内结束")
}
