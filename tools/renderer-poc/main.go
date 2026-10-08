package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"os"

	"github.com/go-webengine/engine"
)

func main() {
	input := flag.String("input", "fixture.html", "HTML file to render")
	output := flag.String("output", "screenshot.png", "PNG output path")
	width := flag.Int("width", 1440, "viewport width")
	height := flag.Int("height", 900, "minimum output height")
	flag.Parse()

	html, err := os.ReadFile(*input)
	if err != nil {
		panic(err)
	}
	renderer := engine.New()
	renderer.DisableJS = true
	result, _, err := renderer.RenderHTML(context.Background(), string(html), "https://mimic.test/", image.Rect(0, 0, *width, *height))
	if err != nil {
		panic(err)
	}
	if result.Bounds().Dy() > *height {
		result = result.SubImage(image.Rect(0, 0, result.Bounds().Dx(), *height)).(*image.RGBA)
	}
	png, err := engine.EncodePNG(result)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(*output, png, 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s (%d bytes, %dx%d)\n", *output, len(png), result.Bounds().Dx(), result.Bounds().Dy())
}
