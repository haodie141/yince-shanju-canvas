package protocol

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFadaImageRequestAndResponse(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin-packages", "fada-image", "manifest.json"))
	if err != nil { t.Fatal(err) }
	adapters, err := LoadInstalledProviders(data, nil)
	if err != nil { t.Fatal(err) }
	for _, images := range [][]MediaReference{nil, {{URL: "https://example.com/one.png"}, {URL: "https://example.com/two.png", Order: 1}}} {
		spec, err := adapters[0].BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "image2.6-1k", Prompt: "test", AspectRatio: "9:16", Images: images}})
		if err != nil { t.Fatal(err) }
		body := manifestTestBody(t, spec)
		if spec.Path != "/v1/images/generations" || spec.ContentType != "application/json" || body["size"] != "9:16" { t.Fatalf("unexpected request: %#v %#v", spec, body) }
		if len(images) == 0 && body["image"] != nil { t.Fatalf("text request has image: %#v", body) }
		if len(images) != 0 { refs, ok := body["image"].([]any); if !ok || len(refs) != 2 || refs[0] != images[0].URL || refs[1] != images[1].URL { t.Fatalf("references: %#v", body["image"]) } }
	}
	for _, tc := range []struct{ input, url, data string }{
		{`{"data":[{"b64_json":"https://example.com/image.png"}]}`, "https://example.com/image.png", ""},
		{`{"data":[{"b64_json":"aW1hZ2U="}]}`, "", "data:image/png;base64,aW1hZ2U="},
		{`{"data":[{"url":"https://example.com/image.png"}]}`, "https://example.com/image.png", ""},
	} {
		result, err := adapters[0].ParseCreate(context.Background(), []byte(tc.input))
		if err != nil { t.Fatal(err) }
		if result.Result == nil || len(result.Result.Images) != 1 || result.Result.Images[0].URL != tc.url || result.Result.Images[0].DataURL != tc.data { t.Fatalf("response: %#v", result.Result) }
	}
}
