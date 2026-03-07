package util

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"sort"
	"time"

	tritongprc "github.com/elijahnyp/home_controller/triton/generated"
	"golang.org/x/image/draw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TritonDetection holds a single object detection result.
type TritonDetection struct {
	Label      string
	Confidence float32
	XMin       int
	YMin       int
	XMax       int
	YMax       int
}

// TritonClient manages the gRPC connection to a Triton Inference Server.
type TritonClient struct {
	conn   *grpc.ClientConn
	client tritongprc.GRPCInferenceServiceClient
}

var tritonClient *TritonClient

// InitTritonClient creates the gRPC connection to the Triton server.
// Call this once at startup, and again whenever config changes.
func InitTritonClient() error {
	addr := Config.GetString("triton_url")
	if addr == "" {
		addr = "10.0.4.226:8001"
	}

	// A 640×640×3 float32 input is ~4.7 MB; set limits well above that.
	const maxMsgSize = 32 * 1024 * 1024 // 32 MB
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(maxMsgSize),
			grpc.MaxCallRecvMsgSize(maxMsgSize),
		),
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return fmt.Errorf("triton: failed to connect to %s: %w", addr, err)
	}

	if tritonClient != nil && tritonClient.conn != nil {
		_ = tritonClient.conn.Close()
	}

	tritonClient = &TritonClient{
		conn:   conn,
		client: tritongprc.NewGRPCInferenceServiceClient(conn),
	}
	Logger.Info().Msgf("Triton gRPC client connected to %s", addr)
	return nil
}

// cocoClasses maps COCO class indices to human-readable names (80 classes).
var cocoClasses = []string{
	"person", "bicycle", "car", "motorcycle", "airplane", "bus", "train",
	"truck", "boat", "traffic light", "fire hydrant", "stop sign",
	"parking meter", "bench", "bird", "cat", "dog", "horse", "sheep", "cow",
	"elephant", "bear", "zebra", "giraffe", "backpack", "umbrella", "handbag",
	"tie", "suitcase", "frisbee", "skis", "snowboard", "sports ball", "kite",
	"baseball bat", "baseball glove", "skateboard", "surfboard", "tennis racket",
	"bottle", "wine glass", "cup", "fork", "knife", "spoon", "bowl", "banana",
	"apple", "sandwich", "orange", "broccoli", "carrot", "hot dog", "pizza",
	"donut", "cake", "chair", "couch", "potted plant", "bed", "dining table",
	"toilet", "tv", "laptop", "mouse", "remote", "keyboard", "cell phone",
	"microwave", "oven", "toaster", "sink", "refrigerator", "book", "clock",
	"vase", "scissors", "teddy bear", "hair drier", "toothbrush",
}

// DetectObjects submits a JPEG image to the Triton Inference Server running
// YOLO11 and returns the detected objects above the configured confidence
// threshold.  The caller is responsible for converting the raw JPEG bytes.
func DetectObjects(jpegData []byte) ([]TritonDetection, error) {
	if tritonClient == nil {
		return nil, fmt.Errorf("triton client not initialized")
	}

	// --- config ----------------------------------------------------------
	modelName := Config.GetString("triton_model")
	if modelName == "" {
		modelName = "yolo11"
	}
	modelVersion := Config.GetString("triton_model_version")
	inputWidth := Config.GetInt("triton_input_width")
	if inputWidth <= 0 {
		inputWidth = 640
	}
	inputHeight := Config.GetInt("triton_input_height")
	if inputHeight <= 0 {
		inputHeight = 640
	}
	minConf := float32(Config.GetFloat64("min_confidence"))
	if minConf <= 0 {
		minConf = 0.5
	}
	iouThresh := float32(Config.GetFloat64("triton_iou_threshold"))
	if iouThresh <= 0 {
		iouThresh = 0.45
	}
	inputTensorName := Config.GetString("triton_input_name")
	if inputTensorName == "" {
		inputTensorName = "images"
	}
	outputTensorName := Config.GetString("triton_output_name")
	if outputTensorName == "" {
		outputTensorName = "output0"
	}

	// --- preprocessing ---------------------------------------------------
	imgRaw, _, err := image.Decode(bytes.NewReader(jpegData))
	if err != nil {
		return nil, fmt.Errorf("triton: decode image: %w", err)
	}
	origW := imgRaw.Bounds().Dx()
	origH := imgRaw.Bounds().Dy()

	// Letterbox resize to keep aspect ratio, pad with grey (114).
	resized, scale, padX, padY := letterboxResize(imgRaw, inputWidth, inputHeight)

	// Convert to NCHW float32 normalised to [0, 1].
	rawInput := imageToNCHW(resized, inputWidth, inputHeight)

	// Build raw bytes (little-endian float32).
	rawBytes := make([]byte, len(rawInput)*4)
	for i, v := range rawInput {
		binary.LittleEndian.PutUint32(rawBytes[i*4:], math.Float32bits(v))
	}

	// --- gRPC inference request ------------------------------------------
	req := &tritongprc.ModelInferRequest{
		ModelName:    modelName,
		ModelVersion: modelVersion,
		Inputs: []*tritongprc.ModelInferRequest_InferInputTensor{
			{
				Name:     inputTensorName,
				Datatype: "FP32",
				Shape:    []int64{1, 3, int64(inputHeight), int64(inputWidth)},
			},
		},
		Outputs: []*tritongprc.ModelInferRequest_InferRequestedOutputTensor{
			{Name: outputTensorName},
		},
		RawInputContents: [][]byte{rawBytes},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := tritonClient.client.ModelInfer(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("triton: ModelInfer RPC: %w", err)
	}

	// --- parse output tensor ---------------------------------------------
	// YOLO11 exports to ONNX with shape [1, 4+numClasses, numAnchors].
	// Triton returns raw bytes in RawOutputContents.
	if len(resp.Outputs) == 0 || len(resp.RawOutputContents) == 0 {
		return nil, fmt.Errorf("triton: empty response outputs")
	}

	outTensor := resp.Outputs[0]
	rawOut := resp.RawOutputContents[0]

	if len(outTensor.Shape) < 3 {
		return nil, fmt.Errorf("triton: unexpected output shape rank %d", len(outTensor.Shape))
	}
	// shape: [batch, rows, anchors]  – rows = 4 + numClasses
	numRows := int(outTensor.Shape[1])    // 4 + numClasses
	numAnchors := int(outTensor.Shape[2]) // e.g. 8400
	numClasses := numRows - 4

	if numClasses <= 0 {
		return nil, fmt.Errorf("triton: invalid output shape: rows=%d", numRows)
	}

	floats, err := rawBytesToFloat32(rawOut)
	if err != nil {
		return nil, fmt.Errorf("triton: parse output tensor: %w", err)
	}

	// --- decode detections -----------------------------------------------
	var detections []detBox
	for a := 0; a < numAnchors; a++ {
		// Each anchor: cx, cy, w, h, cls0 ... cls(n-1)
		cx := floats[0*numAnchors+a]
		cy := floats[1*numAnchors+a]
		w := floats[2*numAnchors+a]
		h := floats[3*numAnchors+a]

		// Find best class.
		bestClass := 0
		bestScore := float32(0)
		for c := 0; c < numClasses; c++ {
			score := floats[(4+c)*numAnchors+a]
			if score > bestScore {
				bestScore = score
				bestClass = c
			}
		}

		if bestScore < minConf {
			continue
		}

		// Convert center format → xyxy (still in model-input pixel space).
		x1 := cx - w/2
		y1 := cy - h/2
		x2 := cx + w/2
		y2 := cy + h/2
		detections = append(detections, detBox{x1, y1, x2, y2, bestScore, bestClass})
	}

	// Non-Maximum Suppression.
	kept := nms(detections, iouThresh)

	// Convert coordinates back to original image space.
	var results []TritonDetection
	for _, d := range kept {
		// Remove letterbox padding, undo scale.
		ox1 := int(math.Round(float64((d.x1 - float32(padX)) / float32(scale)))) //nolint:mnd
		oy1 := int(math.Round(float64((d.y1 - float32(padY)) / float32(scale)))) //nolint:mnd
		ox2 := int(math.Round(float64((d.x2 - float32(padX)) / float32(scale)))) //nolint:mnd
		oy2 := int(math.Round(float64((d.y2 - float32(padY)) / float32(scale)))) //nolint:mnd

		// Clamp to original image bounds.
		ox1 = clampInt(ox1, 0, origW)
		oy1 = clampInt(oy1, 0, origH)
		ox2 = clampInt(ox2, 0, origW)
		oy2 = clampInt(oy2, 0, origH)

		label := "unknown"
		if d.classIdx < len(cocoClasses) {
			label = cocoClasses[d.classIdx]
		}

		results = append(results, TritonDetection{
			Label:      label,
			Confidence: d.conf,
			XMin:       ox1,
			YMin:       oy1,
			XMax:       ox2,
			YMax:       oy2,
		})
	}

	return results, nil
}

// letterboxResize resizes src to fit inside a targetW×targetH canvas while
// preserving aspect ratio, padding the borders with grey (114,114,114).
// Returns the padded image along with the scale factor and (padX, padY) offsets.
func letterboxResize(src image.Image, targetW, targetH int) (image.Image, float64, int, int) {
	srcW := src.Bounds().Dx()
	srcH := src.Bounds().Dy()

	scale := math.Min(float64(targetW)/float64(srcW), float64(targetH)/float64(srcH))
	newW := int(math.Round(float64(srcW) * scale))
	newH := int(math.Round(float64(srcH) * scale))

	padX := (targetW - newW) / 2
	padY := (targetH - newH) / 2

	// Fill with grey.
	canvas := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		for x := 0; x < targetW; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: 114, G: 114, B: 114, A: 255})
		}
	}

	// Scale src into the canvas.
	dst := image.Rect(padX, padY, padX+newW, padY+newH)
	draw.BiLinear.Scale(canvas, dst, src, src.Bounds(), draw.Over, nil)

	return canvas, scale, padX, padY
}

// imageToNCHW converts an image.Image to a CHW float32 slice normalised to
// [0, 1] in RGB channel order.
func imageToNCHW(img image.Image, w, h int) []float32 {
	out := make([]float32, 3*h*w)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			out[0*h*w+y*w+x] = float32(r>>8) / 255.0 //nolint:mnd
			out[1*h*w+y*w+x] = float32(g>>8) / 255.0 //nolint:mnd
			out[2*h*w+y*w+x] = float32(b>>8) / 255.0 //nolint:mnd
		}
	}
	return out
}

// rawBytesToFloat32 interprets a byte slice as little-endian float32 values.
func rawBytesToFloat32(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("byte slice length %d is not a multiple of 4", len(b))
	}
	f := make([]float32, len(b)/4)
	for i := range f {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		f[i] = math.Float32frombits(bits)
	}
	return f, nil
}

// iou computes Intersection-over-Union of two axis-aligned boxes.
func iou(x1a, y1a, x2a, y2a, x1b, y1b, x2b, y2b float32) float32 {
	ix1 := max32(x1a, x1b)
	iy1 := max32(y1a, y1b)
	ix2 := min32(x2a, x2b)
	iy2 := min32(y2a, y2b)
	interW := max32(0, ix2-ix1)
	interH := max32(0, iy2-iy1)
	inter := interW * interH
	areaA := (x2a - x1a) * (y2a - y1a)
	areaB := (x2b - x1b) * (y2b - y1b)
	union := areaA + areaB - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

type detBox struct {
	x1, y1, x2, y2 float32
	conf           float32
	classIdx       int
}

// nms applies per-class greedy Non-Maximum Suppression.
func nms(dets []detBox, iouThresh float32) []detBox {
	// Sort descending by confidence.
	sort.Slice(dets, func(i, j int) bool { return dets[i].conf > dets[j].conf })

	suppressed := make([]bool, len(dets))
	var kept []detBox
	for i, d := range dets {
		if suppressed[i] {
			continue
		}
		kept = append(kept, d)
		for j := i + 1; j < len(dets); j++ {
			if suppressed[j] || dets[j].classIdx != d.classIdx {
				continue
			}
			if iou(d.x1, d.y1, d.x2, d.y2, dets[j].x1, dets[j].y1, dets[j].x2, dets[j].y2) > iouThresh {
				suppressed[j] = true
			}
		}
	}
	return kept
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// EncodeJPEG encodes an image.Image to JPEG bytes (quality 85).
// Exposed so the occupancy layer can re-encode the letterboxed preview if needed.
func EncodeJPEG(img image.Image) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
