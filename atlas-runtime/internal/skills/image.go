package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"atlas-runtime-go/internal/creds"
)

func (r *Registry) registerImage() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "image.generate",
			Description: "Generate an image from a text prompt using OpenAI DALL-E 3. Returns the image URL.",
			Properties: map[string]ToolParam{
				"prompt": {Description: "A description of the image to generate", Type: "string"},
				"size": {
					Description: "Image size: 1024x1024, 1792x1024, or 1024x1792 (default 1024x1024)",
					Type:        "string",
					Enum:        []string{"1024x1024", "1792x1024", "1024x1792"},
				},
				"quality": {
					Description: "Image quality: standard or hd (default standard)",
					Type:        "string",
					Enum:        []string{"standard", "hd"},
				},
				"n": {Description: "Number of images to generate (1–4, default 1)", Type: "integer"},
			},
			Required: []string{"prompt"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          imageGenerate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "image.edit",
			Description: "Edit an existing image using a text instruction via OpenAI DALL-E 2 images/edits endpoint. The source image must be a local PNG file path.",
			Properties: map[string]ToolParam{
				"imagePath": {Description: "Absolute path to the source PNG image file", Type: "string"},
				"prompt":    {Description: "Editing instruction describing the change to make", Type: "string"},
				"size": {
					Description: "Output image size (default 1024x1024)",
					Type:        "string",
					Enum:        []string{"256x256", "512x512", "1024x1024"},
				},
				"n": {Description: "Number of edited images to generate (1–4, default 1)", Type: "integer"},
			},
			Required: []string{"imagePath", "prompt"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassExternalSideEffect,
		Fn:          imageEdit,
	})
}

func imageGenerate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt  string `json:"prompt"`
		Size    string `json:"size"`
		Quality string `json:"quality"`
		N       int    `json:"n"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if p.Size == "" {
		p.Size = "1024x1024"
	}
	if p.Quality == "" {
		p.Quality = "standard"
	}
	if p.N <= 0 {
		p.N = 1
	}
	if p.N > 4 {
		p.N = 4
	}

	bundle, _ := creds.Read()
	if bundle.OpenAIAPIKey == "" {
		return "", fmt.Errorf("OpenAI API key not configured — add it in Settings → Credentials")
	}

	return dalleGenerate(p.Prompt, p.Size, p.Quality, p.N, bundle.OpenAIAPIKey)
}

func imageEdit(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ImagePath string `json:"imagePath"`
		Prompt    string `json:"prompt"`
		Size      string `json:"size"`
		N         int    `json:"n"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.ImagePath == "" || p.Prompt == "" {
		return "", fmt.Errorf("imagePath and prompt are required")
	}
	if p.Size == "" {
		p.Size = "1024x1024"
	}
	if p.N <= 0 {
		p.N = 1
	}
	if p.N > 4 {
		p.N = 4
	}

	bundle, _ := creds.Read()
	if bundle.OpenAIAPIKey == "" {
		return "", fmt.Errorf("OpenAI API key not configured — add it in Settings → Credentials")
	}

	// Resolve and validate the image path
	absPath, err := filepath.Abs(p.ImagePath)
	if err != nil {
		return "", fmt.Errorf("invalid image path: %w", err)
	}
	imgFile, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("cannot open image file: %w", err)
	}
	defer imgFile.Close()

	// Build multipart form
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	imgPart, err := w.CreateFormFile("image", filepath.Base(absPath))
	if err != nil {
		return "", fmt.Errorf("multipart create failed: %w", err)
	}
	if _, err := io.Copy(imgPart, imgFile); err != nil {
		return "", fmt.Errorf("image read failed: %w", err)
	}
	w.WriteField("prompt", p.Prompt)          //nolint:errcheck
	w.WriteField("n", fmt.Sprintf("%d", p.N)) //nolint:errcheck
	w.WriteField("size", p.Size)              //nolint:errcheck
	w.WriteField("model", "dall-e-2")         //nolint:errcheck
	w.Close()

	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/images/edits", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+bundle.OpenAIAPIKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct{ Message string `json:"message"` } `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("OpenAI Images Edit API: %s", errResp.Error.Message)
		}
		return "", fmt.Errorf("OpenAI Images Edit API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	if len(result.Data) == 0 {
		return "No edited images returned.", nil
	}
	out := ""
	for i, img := range result.Data {
		if len(result.Data) > 1 {
			out += fmt.Sprintf("Image %d: %s\n", i+1, img.URL)
		} else {
			out += img.URL
		}
	}
	return out, nil
}

func dalleGenerate(prompt, size, quality string, n int, apiKey string) (string, error) {
	payload := map[string]any{
		"model":   "dall-e-3",
		"prompt":  prompt,
		"n":       n,
		"size":    size,
		"quality": quality,
	}
	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/images/generations", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("OpenAI Images API: %s", errResp.Error.Message)
		}
		return "", fmt.Errorf("OpenAI Images API returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			URL           string `json:"url"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return string(respBody), nil
	}
	if len(result.Data) == 0 {
		return "No images generated.", nil
	}

	out := ""
	for i, img := range result.Data {
		if len(result.Data) > 1 {
			out += fmt.Sprintf("Image %d: %s\n", i+1, img.URL)
		} else {
			out += img.URL
		}
		if img.RevisedPrompt != "" && img.RevisedPrompt != prompt {
			out += "\nRevised prompt: " + img.RevisedPrompt
		}
	}
	return out, nil
}
