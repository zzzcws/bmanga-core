package prototype

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func encodedTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	source := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, source); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return output.Bytes()
}

func TestReaderSourceQualityRequested(t *testing.T) {
	for _, testCase := range []struct {
		name string
		url  string
		want bool
	}{
		{name: "reader source", url: "/page?quality=source", want: true},
		{name: "case insensitive", url: "/page?quality=SOURCE", want: true},
		{name: "reader default", url: "/page?max=2400", want: false},
		{name: "cover cannot opt in", url: "/cover?quality=source", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.url, nil)
			if got := readerSourceQualityRequested(request); got != testCase.want {
				t.Fatalf("readerSourceQualityRequested() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestSourceQualityBypassesReencodeWhenImageAlreadyFits(t *testing.T) {
	data := encodedTestPNG(t, 160, 90)
	request := httptest.NewRequest(http.MethodGet, "/page?quality=source", nil)
	response := httptest.NewRecorder()

	if (&Server{}).sendThumbnailBytes(response, request, data, "image/png", "page.png", time.Now(), 3200) {
		t.Fatal("source-quality page unexpectedly generated a thumbnail")
	}
	if got := response.Header().Get("X-Bmanga-Image-Mode"); got != "source" {
		t.Fatalf("X-Bmanga-Image-Mode = %q, want source", got)
	}
}

func TestImageDimensionFitHelpers(t *testing.T) {
	data := encodedTestPNG(t, 160, 90)
	if !imageBytesWithinMaxDimension(data, 160) {
		t.Fatal("imageBytesWithinMaxDimension rejected an image at the limit")
	}
	if imageBytesWithinMaxDimension(data, 159) {
		t.Fatal("imageBytesWithinMaxDimension accepted an oversized image")
	}
	if imageBytesWithinMaxDimension([]byte("not an image"), 3200) {
		t.Fatal("imageBytesWithinMaxDimension accepted invalid image data")
	}

	path := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}
	if !imageFileWithinMaxDimension(path, 3200) {
		t.Fatal("imageFileWithinMaxDimension rejected a valid image")
	}
	if imageFileWithinMaxDimension(path, 80) {
		t.Fatal("imageFileWithinMaxDimension accepted an oversized image")
	}
}
