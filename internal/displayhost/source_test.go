package displayhost

import (
	"context"
	"testing"

	"AVDDesktop/internal/emulatorgrpc"
)

func TestFrameSourceReuseAndDuplicateRelease(t *testing.T) {
	region, err := newRegion("test-buffer-pool", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = region.Close() }()

	meta := make(chan emulatorgrpc.FrameMeta, 3)
	for seq := uint32(1); seq <= 3; seq++ {
		meta <- emulatorgrpc.FrameMeta{Width: 2, Height: 2, Seq: seq}
	}
	close(meta)

	source := &frameSource{
		ctx:    context.Background(),
		region: region,
		meta:   meta,
		width:  2,
		height: 2,
	}
	first, err := source.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	firstPix := &first.Pix[0]

	source.Release(first)
	// 重复 Release 不能把同一缓冲二次放回池中。
	source.Release(first)

	second, err := source.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := &second.Pix[0]; got != firstPix {
		t.Fatal("released frame buffer was not reused")
	}

	// 若重复 Release 生效，这里的池会把同一缓冲再次取出。第三帧必须换一块缓冲。
	third, err := source.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := &third.Pix[0]; got == firstPix {
		t.Fatal("duplicate release returned the same in-use buffer twice")
	}

	source.Release(second)
	source.Release(third)
	source.Release(Frame{})
}
