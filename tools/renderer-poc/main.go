package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"github.com/go-webengine/engine/static"
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
	renderer := static.New()
	result, err := renderer.RenderHTML(context.Background(), string(html), "https://mimic.test/", image.Rect(0, 0, *width, *height))
	if err != nil {
		panic(err)
	}
	if result.Bounds().Dy() > *height {
		result = result.SubImage(image.Rect(0, 0, result.Bounds().Dx(), *height)).(*image.RGBA)
	}
	file, err := os.Create(*output)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	if err := png.Encode(file, result); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s (%dx%d)\n", *output, result.Bounds().Dx(), result.Bounds().Dy())
}
