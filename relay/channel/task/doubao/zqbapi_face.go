package doubao

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"sync"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	pigo "github.com/esimov/pigo/core"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	zqbapiFaceImageMaxSide      = 640
	zqbapiFaceScore             = 5.0
	zqbapiImageMinDimension     = 300
	zqbapiImageMaxDimension     = 6000
	zqbapiImageNormalizeMinSide = 512
	zqbapiImageNormalizeMaxSide = 4096
	zqbapiImageMaxBytes         = 30 * 1024 * 1024
	zqbapiImageDecodeMaxPixels  = 64_000_000
	zqbapiImageNormalizationV2  = "v2"
)

// facefinder is the Pigo face cascade distributed under the MIT license.
// See assets/LICENSE.pigo.
//
//go:embed assets/facefinder
var zqbapiFaceCascade []byte

var (
	zqbapiFaceClassifier     *pigo.Pigo
	zqbapiFaceClassifierErr  error
	zqbapiFaceClassifierOnce sync.Once
)

type zqbapiImageInspection struct {
	Data                 []byte
	MIMEType             string
	HasFace              bool
	Width                int
	Height               int
	Size                 int
	Normalized           bool
	NormalizationVersion string
}

func inspectZQBAPIImage(c *gin.Context, input string) (*zqbapiImageInspection, error) {
	source := types.NewFileSourceFromData(input, "")
	fileData, err := service.LoadFileSource(c, source, "ZQBAPI local face detection")
	if err != nil {
		return nil, fmt.Errorf("load image for face detection: %w", err)
	}
	if c == nil {
		defer fileData.Close()
	}
	encoded, err := fileData.GetBase64Data()
	if err != nil {
		return nil, fmt.Errorf("read image for face detection: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode image for face detection: %w", err)
	}
	config, err := decodeZQBAPIImageConfig(data)
	if err != nil {
		return nil, newZQBAPIBuildError(zqbapiErrorInvalidImage, "inspect", err)
	}
	if err := validateZQBAPIImageConfig(config.Width, config.Height); err != nil {
		return nil, newZQBAPIBuildError(zqbapiErrorInvalidImage, "inspect", err)
	}
	if int64(config.Width)*int64(config.Height) > zqbapiImageDecodeMaxPixels {
		return nil, newZQBAPIBuildError(zqbapiErrorInvalidImage, "inspect", fmt.Errorf("image has too many pixels: %dx%d", config.Width, config.Height))
	}

	img, err := decodeZQBAPIImage(data)
	if err != nil {
		return nil, newZQBAPIBuildError(zqbapiErrorInvalidImage, "inspect", err)
	}
	img, orientationNormalized := applyZQBAPIEXIFOrientation(data, img)
	normalizedData, normalizedMIME, normalizedImage, normalized, err := normalizeZQBAPIImage(data, fileData.MimeType, img, orientationNormalized)
	if err != nil {
		return nil, newZQBAPIBuildError(zqbapiErrorInvalidImage, "normalize", err)
	}
	normalizedBounds := normalizedImage.Bounds()
	inspection := &zqbapiImageInspection{
		Data:                 normalizedData,
		MIMEType:             normalizedMIME,
		Width:                normalizedBounds.Dx(),
		Height:               normalizedBounds.Dy(),
		Size:                 len(normalizedData),
		Normalized:           normalized,
		NormalizationVersion: zqbapiImageNormalizationV2,
	}

	detectionImage := resizeZQBAPIImage(normalizedImage, zqbapiFaceImageMaxSide)

	classifier, err := getZQBAPIFaceClassifier()
	if err != nil {
		return nil, err
	}
	bounds := detectionImage.Bounds()
	rows, cols := bounds.Dy(), bounds.Dx()
	minDimension := min(rows, cols)
	if minDimension < 20 {
		return inspection, nil
	}
	minFaceSize := max(24, minDimension/20)
	params := pigo.CascadeParams{
		MinSize:     minFaceSize,
		MaxSize:     minDimension,
		ShiftFactor: 0.1,
		ScaleFactor: 1.1,
		ImageParams: pigo.ImageParams{
			Pixels: pigo.RgbToGrayscale(detectionImage),
			Rows:   rows,
			Cols:   cols,
			Dim:    cols,
		},
	}
	detections := classifier.RunCascade(params, 0)
	detections = classifier.ClusterDetections(detections, 0.2)
	for _, detection := range detections {
		if detection.Q >= zqbapiFaceScore {
			inspection.HasFace = true
			return inspection, nil
		}
	}
	return inspection, nil
}

func decodeZQBAPIImageConfig(data []byte) (image.Config, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		return config, nil
	}
	config, webpErr := webp.DecodeConfig(bytes.NewReader(data))
	if webpErr == nil {
		return config, nil
	}
	return image.Config{}, fmt.Errorf("unsupported image format: %w", err)
}

func validateZQBAPIImageConfig(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("image dimensions are invalid: %dx%d", width, height)
	}
	ratio := float64(width) / float64(height)
	if ratio <= 0.4 || ratio >= 2.5 {
		return fmt.Errorf("image aspect ratio %.4f is outside the supported range (0.4, 2.5)", ratio)
	}
	return nil
}

func normalizeZQBAPIImage(data []byte, mimeType string, img image.Image, forceEncode bool) ([]byte, string, image.Image, bool, error) {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	needsResize := width <= zqbapiImageMinDimension || height <= zqbapiImageMinDimension ||
		width >= zqbapiImageMaxDimension || height >= zqbapiImageMaxDimension
	needsEncode := forceEncode || needsResize || len(data) >= zqbapiImageMaxBytes
	if !needsEncode {
		return data, normalizeZQBAPIMIME(mimeType), img, false, nil
	}

	targetWidth, targetHeight := width, height
	if width <= zqbapiImageMinDimension || height <= zqbapiImageMinDimension {
		scale := math.Max(float64(zqbapiImageNormalizeMinSide)/float64(width), float64(zqbapiImageNormalizeMinSide)/float64(height))
		targetWidth = max(1, int(math.Round(float64(width)*scale)))
		targetHeight = max(1, int(math.Round(float64(height)*scale)))
	}
	if targetWidth >= zqbapiImageMaxDimension || targetHeight >= zqbapiImageMaxDimension {
		scale := math.Min(float64(zqbapiImageNormalizeMaxSide)/float64(targetWidth), float64(zqbapiImageNormalizeMaxSide)/float64(targetHeight))
		targetWidth = max(1, int(math.Round(float64(targetWidth)*scale)))
		targetHeight = max(1, int(math.Round(float64(targetHeight)*scale)))
	}

	normalizedImage := img
	if targetWidth != width || targetHeight != height {
		dst := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		normalizedImage = dst
	}

	var output bytes.Buffer
	outputMIME := "image/png"
	if zqbapiImageIsOpaque(normalizedImage) {
		outputMIME = "image/jpeg"
		flattened := image.NewRGBA(normalizedImage.Bounds())
		draw.Draw(flattened, flattened.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(flattened, flattened.Bounds(), normalizedImage, normalizedImage.Bounds().Min, draw.Over)
		if err := jpeg.Encode(&output, flattened, &jpeg.Options{Quality: 92}); err != nil {
			return nil, "", nil, false, fmt.Errorf("encode normalized JPEG: %w", err)
		}
	} else if err := png.Encode(&output, normalizedImage); err != nil {
		return nil, "", nil, false, fmt.Errorf("encode normalized PNG: %w", err)
	}
	if output.Len() >= zqbapiImageMaxBytes {
		return nil, "", nil, false, fmt.Errorf("normalized image size %d bytes exceeds the 30MB material limit", output.Len())
	}
	if targetWidth <= zqbapiImageMinDimension || targetHeight <= zqbapiImageMinDimension ||
		targetWidth >= zqbapiImageMaxDimension || targetHeight >= zqbapiImageMaxDimension {
		return nil, "", nil, false, fmt.Errorf("normalized image dimensions remain unsupported: %dx%d", targetWidth, targetHeight)
	}
	return output.Bytes(), outputMIME, normalizedImage, true, nil
}

func applyZQBAPIEXIFOrientation(data []byte, img image.Image) (image.Image, bool) {
	orientation := zqbapiJPEGEXIFOrientation(data)
	if orientation < 2 || orientation > 8 {
		return img, false
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	targetWidth, targetHeight := width, height
	if orientation >= 5 {
		targetWidth, targetHeight = height, width
	}
	target := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			targetX, targetY := x, y
			switch orientation {
			case 2:
				targetX = width - 1 - x
			case 3:
				targetX, targetY = width-1-x, height-1-y
			case 4:
				targetY = height - 1 - y
			case 5:
				targetX, targetY = y, x
			case 6:
				targetX, targetY = height-1-y, x
			case 7:
				targetX, targetY = height-1-y, width-1-x
			case 8:
				targetX, targetY = y, width-1-x
			}
			target.Set(targetX, targetY, img.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return target, true
}

func zqbapiJPEGEXIFOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			offset++
			continue
		}
		marker := data[offset+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		segmentLength := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		if segmentLength < 2 || offset+2+segmentLength > len(data) {
			break
		}
		segment := data[offset+4 : offset+2+segmentLength]
		if marker == 0xe1 && len(segment) >= 14 && bytes.Equal(segment[:6], []byte{'E', 'x', 'i', 'f', 0, 0}) {
			if orientation := zqbapiTIFFOrientation(segment[6:]); orientation >= 1 && orientation <= 8 {
				return orientation
			}
		}
		offset += 2 + segmentLength
	}
	return 1
}

func zqbapiTIFFOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	ifdOffset := uint64(order.Uint32(data[4:8]))
	if ifdOffset+2 > uint64(len(data)) {
		return 1
	}
	entryCount := uint64(order.Uint16(data[ifdOffset : ifdOffset+2]))
	entriesStart := ifdOffset + 2
	if entryCount > (uint64(len(data))-entriesStart)/12 {
		return 1
	}
	for index := uint64(0); index < entryCount; index++ {
		entryOffset := entriesStart + index*12
		entry := data[entryOffset : entryOffset+12]
		if order.Uint16(entry[:2]) == 0x0112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) >= 1 {
			return int(order.Uint16(entry[8:10]))
		}
	}
	return 1
}

func normalizeZQBAPIMIME(mimeType string) string {
	switch mimeType {
	case "image/jpg", "image/jpeg":
		return "image/jpeg"
	case "image/png", "image/gif", "image/webp":
		return mimeType
	default:
		return mimeType
	}
}

func zqbapiImageIsOpaque(img image.Image) bool {
	if opaque, ok := img.(interface{ Opaque() bool }); ok {
		return opaque.Opaque()
	}
	return false
}

func getZQBAPIFaceClassifier() (*pigo.Pigo, error) {
	zqbapiFaceClassifierOnce.Do(func() {
		zqbapiFaceClassifier, zqbapiFaceClassifierErr = pigo.NewPigo().Unpack(zqbapiFaceCascade)
	})
	if zqbapiFaceClassifierErr != nil {
		return nil, fmt.Errorf("initialize face detector: %w", zqbapiFaceClassifierErr)
	}
	return zqbapiFaceClassifier, nil
}

func decodeZQBAPIImage(data []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err == nil {
		return img, nil
	}
	img, webpErr := webp.Decode(bytes.NewReader(data))
	if webpErr == nil {
		return img, nil
	}
	return nil, fmt.Errorf("unsupported image for face detection: %w", err)
}

func resizeZQBAPIImage(src image.Image, maxSide int) image.Image {
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maxSide && height <= maxSide {
		return src
	}
	newWidth, newHeight := width, height
	if width >= height {
		newWidth = maxSide
		newHeight = max(1, height*maxSide/width)
	} else {
		newHeight = maxSide
		newWidth = max(1, width*maxSide/height)
	}
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}
