package protocol

import (
    "context"
    "os"
    "path/filepath"
    "testing"
)

func TestShanju5090ReferenceAudio(t *testing.T) {
    data, err := os.ReadFile(filepath.Join("..", "..", "..", "plugin-packages", "shanju-h3-5090-fast", "manifest.json"))
    if err != nil { t.Fatal(err) }
    adapter, err := LoadInstalledPlugin(data, nil)
    if err != nil { t.Fatal(err) }
    for _, tc := range []struct { name string; request GenerationRequest; mode, contentType string; audioCount int }{
        {"text", GenerationRequest{Prompt:"test"}, "text", "application/json", 0},
        {"audio-only", GenerationRequest{Prompt:"test", Audios:[]MediaReference{{URL:"https://example.com/audio.wav"}}}, "reference", "multipart/form-data", 1},
        {"image-and-three-audio", GenerationRequest{Prompt:"test", Images:[]MediaReference{{URL:"https://example.com/image.png",Role:"reference_image"}}, Audios:[]MediaReference{{URL:"https://example.com/1.wav"},{URL:"https://example.com/2.wav"},{URL:"https://example.com/3.wav"}}}, "reference", "multipart/form-data", 3},
        {"first-frame", GenerationRequest{Prompt:"test", Images:[]MediaReference{{URL:"https://example.com/first.png",Role:"first_frame"}}}, "first_frame", "multipart/form-data", 0},
    } {
        t.Run(tc.name, func(t *testing.T) {
            spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request:tc.request})
            if err != nil { t.Fatal(err) }
            body := manifestTestBody(t,spec)
            if body["mode"] != tc.mode || spec.ContentType != tc.contentType { t.Fatalf("mode=%v contentType=%v",body["mode"],spec.ContentType) }
            count:=0
            for _, f := range spec.Files { if f.Name=="reference_audio" { count++ } }
            if count!=tc.audioCount { t.Fatalf("got %d audio files, want %d",count,tc.audioCount) }
        })
    }
}
