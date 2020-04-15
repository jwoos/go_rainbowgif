package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"runtime"
	"strings"

	"github.com/lucasb-eyer/go-colorful"
)

/* frame is just `*image.Paletted`
 * `color.Palette` is just `[]color.Color`
 * `color.Color` is an interface implementing `RGBA()`
 */
func prepareFrame(src *image.Paletted, dst *image.Paletted, overlayColor colorful.Color) {
	dst.Pix = src.Pix
	dst.Stride = src.Stride
	dst.Rect = src.Rect
	dst.Palette = make([]color.Color, len(src.Palette))

	for pixelIndex, pixel := range src.Palette {
		_, _, _, alpha := pixel.RGBA()
		convertedPixel, ok := colorful.MakeColor(pixel)

		if alpha == 0 || !ok {
			dst.Palette[pixelIndex] = pixel
			continue
		}

		convertedPixel = convertedPixel.Clamped()

		blendedPixel := blendColor(overlayColor, convertedPixel)

		blendedR, blendedG, blendedB := blendedPixel.RGB255()
		dst.Palette[pixelIndex] = color.NRGBA{
			blendedR,
			blendedG,
			blendedB,
			255,
		}
	}
}

func staticImageTransform() {

}

func parseGradientColors(gradientColors string) ([]colorful.Color, error) {
	var colors []colorful.Color

	if len(gradientColors) != 0 {
		colorHexes := strings.Split(gradientColors, ",")
		colors = make([]colorful.Color, len(colorHexes))
		for i, hex := range colorHexes {
			color, err := colorful.Hex("#" + hex)
			if err != nil {
				return nil, errors.New(fmt.Sprintf("Invalid color: %s", hex))
			}
			colors[i] = color
		}
	} else {
		// ROYGBV
		colors = []colorful.Color{
			{1, 0, 0},
			{1, 127.0 / 255.0, 0},
			{1, 1, 0},
			{0, 1, 0},
			{0, 0, 1},
			{139.0 / 255.0, 0, 1},
		}
	}

	return colors, nil
}

func main() {
	// register image formats
	image.RegisterFormat("jpeg", "\xFF\xD8", jpeg.Decode, jpeg.DecodeConfig)
	image.RegisterFormat("png", "\x89\x50\x4E\x47\x0D\x0A\x1A\x0A", png.Decode, png.DecodeConfig)
	image.RegisterFormat("gif", "\x47\x49\x46\x38\x39\x61", gif.Decode, gif.DecodeConfig)

	var threads int
	flag.IntVar(&threads, "threads", runtime.NumCPU()/2, "The number of go threads to use")

	var gradientColors string
	flag.StringVar(&gradientColors, "gradient", "", "A list of colors in hex without # separated by comma to use as the gradient")

	var loopCount int
	flag.IntVar(&loopCount, "loop_count", 1, "The number of times ot loop through thr GIF")

	flag.Parse()

	if threads <= 0 {
		fmt.Println("Thread count must be at least 1")
		os.Exit(1)
	}

	colors, err := parseGradientColors(gradientColors)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if loopCount < 1 {
		fmt.Println("Loop count must be at least 1")
		os.Exit(1)
	}

	positionalArgs := flag.Args()

	if len(positionalArgs) != 2 {
		fmt.Println("Expected two positional arguments: input and output")
		os.Exit(1)
	}

	input := positionalArgs[0]
	output := positionalArgs[1]

	file, err := os.Open(input)
	if err != nil {
		fmt.Println("Error opening file: ", err)
		os.Exit(1)
	}

	img, err := gif.DecodeAll(file)
	if err != nil {
		fmt.Println("Error decoding: ", err)
		os.Exit(1)
	}
	file.Close()

	frameCount := len(img.Image) * loopCount
	newFrames := make([]*image.Paletted, frameCount)
	for i := range newFrames {
		newFrames[i] = new(image.Paletted)
	}

	gradient := newGradient(colors, true)
	overlayColors := gradient.generate(frameCount)

	framesPerThread := len(img.Image)/threads + 1
	ch := make(chan int)
	barrier := 0

	frameIndex := 0
	normalizedFrameIndex := 0
	for i := 0; i < threads; i++ {
		go func(base int) {
			processed := 0
			for processed < framesPerThread {
				if frameIndex >= len(newFrames) {
					break
				}

				if normalizedFrameIndex >= len(img.Image) {
					normalizedFrameIndex = 0
				}

				// do actual work in here
				prepareFrame(
					img.Image[normalizedFrameIndex],
					newFrames[frameIndex],
					overlayColors[frameIndex],
				)
				frameIndex++
				normalizedFrameIndex++
			}

			// thread is done
			ch <- 1
		}(i)
	}

	// wait for all threads to synchronize
	for barrier != threads {
		barrier += <-ch
	}

	newDelay := make([]int, len(newFrames))
	for i := range newDelay {
		newDelay[i] = img.Delay[i%len(img.Delay)]
	}

	newDisposal := make([]byte, len(newFrames))
	for i := range newDisposal {
		newDisposal[i] = img.Disposal[i%len(img.Disposal)]
	}

	img.Image = newFrames
	img.Delay = newDelay
	img.Disposal = newDisposal

	file, err = os.OpenFile(output, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		fmt.Println("Error opening file: ", err)
		os.Exit(1)
	}

	img.Config.ColorModel = nil
	img.BackgroundIndex = 0

	err = gif.EncodeAll(file, img)
	if err != nil {
		fmt.Println("Error encoding image: ", err)
		os.Exit(1)
	}
	file.Close()
}
