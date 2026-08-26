package rag

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"mewcode/internal/config"
)

const ocrPrompt = "提取图片中的全部文字内容，完整保留原始语言和数字。如果图片包含表格，将表格输出为 Markdown 表格格式（使用 | 分隔），保留行列结构和表头。如果图片包含图表或示意图，用一两句话描述图表类型和核心信息。公式使用 LaTeX 表示。只输出提取的内容，不要任何解释或前后缀。如果图片中没有可提取的文字内容，输出 [NO_TEXT]。"

const (
	ocrMaxAttempts       = 3
	ocrMaxImagesPerFile  = 20
	ocrMinImageDimension = 100
)

type OcrClient struct {
	client     *openai.Client
	model      string
	callCount  int
	failCount  int
	skipReason []string
}

type NewOcrClientResult struct {
	Client *OcrClient
	Err    error
}

func NewOcrClient(cfg *config.ProviderConfig) *NewOcrClientResult {
	if cfg.VisionModel == "" {
		return &NewOcrClientResult{Client: nil, Err: nil}
	}
	baseURL := cfg.VisionURL
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	apiKey := cfg.ResolveVisionAPIKey()
	if apiKey == "" {
		return &NewOcrClientResult{Client: nil, Err: fmt.Errorf("vision api key not found; set vision_api_key or api_key in config")}
	}
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	return &NewOcrClientResult{Client: &OcrClient{client: &client, model: cfg.VisionModel}, Err: nil}
}

func (o *OcrClient) Model() string {
	if o == nil {
		return ""
	}
	return o.model
}

func (o *OcrClient) Available() bool {
	return o != nil && o.client != nil
}

func (o *OcrClient) RecordSkip(reason string) {
	o.skipReason = append(o.skipReason, reason)
}

func (o *OcrClient) Report() string {
	if o == nil || (o.callCount == 0 && len(o.skipReason) == 0) {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("OCR: %d 张图片处理, %d 张失败", o.callCount, o.failCount))
	if len(o.skipReason) > 0 {
		sb.WriteString(", 跳过: ")
		sb.WriteString(strings.Join(o.skipReason, "; "))
	}
	return sb.String()
}

func (o *OcrClient) Extract(ctx context.Context, imageData []byte, mime string) (string, error) {
	if !o.Available() {
		return "", fmt.Errorf("ocr client not available")
	}
	if mime == "" {
		mime = "image/png"
	}
	dataURL := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(imageData))

	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModel(o.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.TextContentPart(ocrPrompt),
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL: dataURL,
				}),
			}),
		},
	}

	var choice *openai.ChatCompletion
	var err error
	for attempt := 0; attempt < ocrMaxAttempts; attempt++ {
		o.callCount++
		choice, err = o.client.Chat.Completions.New(ctx, params)
		if err == nil {
			break
		}
		if attempt < ocrMaxAttempts-1 {
			backoff := time.Duration(1500*(attempt+1)) * time.Millisecond
			select {
			case <-ctx.Done():
				o.failCount++
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if err != nil {
		o.failCount++
		return "", fmt.Errorf("ocr api call failed: %w", err)
	}
	if len(choice.Choices) == 0 {
		o.failCount++
		return "", fmt.Errorf("ocr api returned no choices")
	}
	text := strings.TrimSpace(choice.Choices[0].Message.Content)
	if text == "[NO_TEXT]" {
		return "", nil
	}
	return text, nil
}
