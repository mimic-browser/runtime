package browser

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/moreveal/mimic/internal/renderer"
)

// ScreenshotClip uses document CSS-pixel coordinates. Scale is deliberately
// absent: this approximate renderer currently emits one pixel per CSS pixel.
type ScreenshotClip struct {
	X, Y, Width, Height float64
}

// CaptureApproximateScreenshot renders an immutable projection of the current
// Page. The renderer performs its own CSS layout, so the PNG is visual context,
// not the Page's modeled geometry or a Chrome-equivalent screenshot.
func (p *Page) CaptureApproximateScreenshot(ctx context.Context, clip *ScreenshotClip) ([]byte, []string, error) {
	width, height := p.env.Window.ViewportWidth, p.env.Window.ViewportHeight
	if width <= 0 || height <= 0 {
		return nil, nil, fmt.Errorf("page has no viewport")
	}
	if clip != nil {
		for _, value := range []float64{clip.X, clip.Y, clip.Width, clip.Height} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, nil, fmt.Errorf("screenshot clip must be finite")
			}
		}
		if clip.X < 0 || clip.Y < 0 || clip.X > 16384 || clip.Y > 16384 || clip.Width <= 0 || clip.Height <= 0 || clip.Width > 4096 || clip.Height > 4096 || clip.Width*clip.Height > 16<<20 {
			return nil, nil, fmt.Errorf("invalid screenshot clip")
		}
	}
	snapshot, err := p.CaptureSnapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	rendered, err := renderer.Render(ctx, snapshot.Files, width, height)
	if err != nil {
		return nil, snapshot.Warnings, err
	}
	region := image.Rect(0, 0, width, height)
	if clip != nil {
		region = image.Rect(int(math.Floor(clip.X)), int(math.Floor(clip.Y)), int(math.Ceil(clip.X+clip.Width)), int(math.Ceil(clip.Y+clip.Height)))
	} else {
		value, err := p.EvaluateCommand(ctx, "", `({x:scrollX,y:scrollY})`)
		if err != nil {
			return nil, snapshot.Warnings, fmt.Errorf("read screenshot scroll offset: %w", err)
		}
		offsets, ok := value.(map[string]any)
		if !ok {
			return nil, snapshot.Warnings, fmt.Errorf("screenshot scroll offset is unavailable")
		}
		x, xOK := offsets["x"].(float64)
		y, yOK := offsets["y"].(float64)
		if !xOK || !yOK {
			return nil, snapshot.Warnings, fmt.Errorf("screenshot scroll offset is unavailable")
		}
		region = region.Add(image.Pt(int(x), int(y)))
	}
	if !region.In(rendered.Bounds()) {
		return nil, snapshot.Warnings, fmt.Errorf("screenshot region exceeds rendered document")
	}
	var output bytes.Buffer
	if err := png.Encode(&output, rendered.SubImage(region)); err != nil {
		return nil, snapshot.Warnings, err
	}
	return output.Bytes(), snapshot.Warnings, nil
}
