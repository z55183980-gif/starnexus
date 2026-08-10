package doubao

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"sync"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	pigo "github.com/esimov/pigo/core"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const (
	zqbapiFaceImageMaxSide = 640
	zqbapiFaceScore        = 5.0
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
	Data     []byte
	MIMEType string
	HasFace  bool
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
	img, err := decodeZQBAPIImage(data)
	if err != nil {
		return nil, err
	}
	img = resizeZQBAPIImage(img, zqbapiFaceImageMaxSide)

	classifier, err := getZQBAPIFaceClassifier()
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	rows, cols := bounds.Dy(), bounds.Dx()
	minDimension := min(rows, cols)
	if minDimension < 20 {
		return &zqbapiImageInspection{Data: data, MIMEType: fileData.MimeType}, nil
	}
	minFaceSize := max(24, minDimension/20)
	params := pigo.CascadeParams{
		MinSize:     minFaceSize,
		MaxSize:     minDimension,
		ShiftFactor: 0.1,
		ScaleFactor: 1.1,
		ImageParams: pigo.ImageParams{
			Pixels: pigo.RgbToGrayscale(img),
			Rows:   rows,
			Cols:   cols,
			Dim:    cols,
		},
	}
	detections := classifier.RunCascade(params, 0)
	detections = classifier.ClusterDetections(detections, 0.2)
	for _, detection := range detections {
		if detection.Q >= zqbapiFaceScore {
			return &zqbapiImageInspection{Data: data, MIMEType: fileData.MimeType, HasFace: true}, nil
		}
	}
	return &zqbapiImageInspection{Data: data, MIMEType: fileData.MimeType}, nil
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
