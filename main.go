package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/unixpickle/essentials"
	"github.com/unixpickle/ffmpego"
)

var ReturnCode = 0

func main() {
	defer func() {
		if ReturnCode != 0 {
			os.Exit(ReturnCode)
		}
	}()

	var bgColorStr string
	var waveColorStr string
	var output string
	var fps int
	var sampleRate int
	var previewSamples int
	var width int
	var height int
	flag.StringVar(&bgColorStr, "bg-color", "#4b5f76", "background color for video")
	flag.StringVar(&waveColorStr, "wave-color", "#c2f1db", "background color for video")
	flag.StringVar(&output, "output", "video.mp4", "output video file")
	flag.IntVar(&fps, "fps", 24, "frame rate")
	flag.IntVar(&sampleRate, "sample-rate", 16000, "audio sample rate")
	flag.IntVar(&previewSamples, "preview-samples", 4000, "number of samples to visualize at a time")
	flag.IntVar(&width, "width", 1920, "width of frame")
	flag.IntVar(&height, "height", 1080, "height of frame")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <caption> <audio file> [<caption> <audio file> ...]", os.Args[0])
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args)%2 != 0 || len(args) == 0 {
		flag.Usage()
	}
	bgColor := ParseColor(bgColorStr)
	waveColor := ParseColor(waveColorStr)

	captions := []string{}
	audioFiles := []string{}
	for i := 0; i < len(args); i += 2 {
		captions = append(captions, args[i])
		audioFiles = append(audioFiles, args[i+1])
	}

	tmpDir, err := ioutil.TempDir("", "soundviz")
	Must(err)
	defer os.RemoveAll(tmpDir)

	joinedAudio := filepath.Join(tmpDir, "joined.m4a")
	allSamples := CombineAudioFiles(joinedAudio, sampleRate, audioFiles)

	vw, err := ffmpego.NewVideoWriterWithAudio(output, width, height, float64(fps), joinedAudio)
	Must(err)
	defer vw.Close()

	t := 0.0
	dt := 1.0 / float64(fps)
	for i := 0; true; i++ {
		log.Println("creating frame", i, "...")
		preview, chunkIndex := PreviewChunk(allSamples, int(t*float64(sampleRate)), previewSamples)
		if chunkIndex == -1 {
			break
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.Set(x, y, bgColor)
			}
		}
		waveLeft := int(float64(width) * 0.1)
		waveWidth := width - waveLeft*2
		waveHeight := int(float64(height) * 0.2)
		waveOffset := int(float64(height) * 0.6)
		for i, sample := range ResampleChunk(preview, waveWidth) {
			height := essentials.MaxInt(2, int(math.Abs(sample)*float64(waveHeight)))
			for y := waveOffset - height; y < waveOffset+height; y++ {
				img.Set(i+waveLeft, y, waveColor)
			}
		}
		Must(vw.WriteFrame(img))
		t += dt
	}
}

func ParseColor(code string) color.Color {
	if len(code) != 7 || code[0] != '#' {
		essentials.Die("invalid color: " + code)
	}
	var nums [3]uint8
	for i := 1; i < 7; i += 2 {
		n, err := strconv.ParseInt(code[i:i+2], 16, 32)
		if err != nil || n < 0 || n > 0xff {
			essentials.Die("invalid color: " + code)
		}
		nums[i/2] = uint8(n)
	}
	return color.RGBA{R: nums[0], G: nums[1], B: nums[2], A: 0xff}
}

func CombineAudioFiles(output string, sampleRate int, audioFiles []string) [][]float64 {
	aw, err := ffmpego.NewAudioWriter(output, sampleRate)
	Must(err)
	defer aw.Close()

	allSamples := [][]float64{}
	for _, file := range audioFiles {
		ar, err := ffmpego.NewAudioReaderResampled(file, sampleRate)
		Must(err)
		defer ar.Close()

		buffer := []float64{}
		for {
			out := make([]float64, 4096)
			n, err := ar.ReadSamples(out)
			if n != 0 {
				buffer = append(buffer, out[:n]...)
				Must(aw.WriteSamples(out[:n]))
			}
			if err == io.EOF {
				break
			}
			Must(err)
		}

		ar.Close() // we will double close, but that's fine
		allSamples = append(allSamples, buffer)
	}
	return allSamples
}

func PreviewChunk(allSamples [][]float64, sampleIdx, numSamples int) ([]float64, int) {
	offset := 0
	for i, chunk := range allSamples {
		if offset+len(chunk) <= sampleIdx {
			offset += len(chunk)
			continue
		}
		startIdx := sampleIdx - offset
		res := make([]float64, numSamples)
		copy(res, chunk[startIdx:])
		return res, i
	}
	return make([]float64, numSamples), -1
}

func ResampleChunk(samples []float64, numSamples int) []float64 {
	stride := float64(len(samples)) / float64(numSamples)
	res := make([]float64, numSamples)
	for i := range res {
		res[i] = samples[int(math.Round(float64(i)*stride))]
	}
	return res
}

func Must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		ReturnCode = 1
		runtime.Goexit()
	}
}
