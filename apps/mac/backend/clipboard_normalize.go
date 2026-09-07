// Copyright (C) 2026 FuseItAll contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3 of the License. See LICENSE
// for details.

package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"strings"
	"time"

	"fuseitall/core"
)

// normalizeNeedsTranscode reports whether mime needs conversion to PNG.
// Only tiff/heic/heif benefit; universal mimes pass through.
func normalizeNeedsTranscode(mime string) bool {
	m := strings.ToLower(strings.TrimSpace(mime))
	return m == "image/tiff" || m == "image/heic" || m == "image/heif"
}

// NormalizeClipImageForSend converts clipboard image to a very normalized
// format before sending to the phone. Contract:
//   - TIFF/HEIC/HEIF → PNG via sips (macOS native) with Go png fallback.
//   - PNG/JPEG/GIF/WEBP pass through.
//   - If normalized PNG exceeds MaxClipImageRaw (5 MiB), re-encode as JPEG q85.
//   - Returns sanitized mime, b64, filename. Fails closed on invalid input.
// Pure aside from sips exec; no cgo.
func NormalizeClipImageForSend(b64, mime, filename string) (outB64, outMime, outFilename string, ok bool) {
	raw, ok := core.SanitizeClipImage(b64, mime)
	if !ok {
		return "", "", "", false
	}
	m, _ := core.SanitizeClipMime(mime)
	fn := core.SanitizeClipFilename(filename)

	needs := normalizeNeedsTranscode(m)
	if !needs {
		// Normalize mime alias (jpg→jpeg) and filename extension.
		synth := core.SynthesizeClipFilename(fn, m, time.Now().Unix())
		// If caller passed a bad filename, synthesize keeps correct ext.
		if fn == "" || !strings.EqualFold(fn, synth) && core.SanitizeClipFilename(fn) == "" {
			fn = core.SanitizeClipFilename(synth)
		}
		// Re-validate output still under cap after alias fix.
		enc := base64.StdEncoding.EncodeToString(raw)
		if len(enc) > core.MaxClipImageB64Len || len(raw) > core.MaxClipImageRaw {
			return "", "", "", false
		}
		if _, vok := core.SanitizeClipImage(enc, m); !vok {
			return "", "", "", false
		}
		return enc, m, fn, true
	}

	// Transcode TIFF/HEIC/HEIF → PNG.
	pngRaw, err := transcodeToPNG(raw)
	if err != nil || len(pngRaw) == 0 {
		return "", "", "", false
	}
	outMimeTry := "image/png"
	// If PNG still >5 MiB, try JPEG q85 (smaller, still whitelisted).
	if len(pngRaw) > core.MaxClipImageRaw {
		if jraw, jerr := encodeJPEG(pngRaw, 85); jerr == nil && len(jraw) > 0 && len(jraw) <= core.MaxClipImageRaw {
			pngRaw = jraw
			outMimeTry = "image/jpeg"
		} else {
			return "", "", "", false
		}
	}
	enc := base64.StdEncoding.EncodeToString(pngRaw)
	if len(enc) > core.MaxClipImageB64Len {
		return "", "", "", false
	}
	if _, vok := core.SanitizeClipImage(enc, outMimeTry); !vok {
		return "", "", "", false
	}
	outFn := core.SynthesizeClipFilename(fn, outMimeTry, time.Now().Unix())
	if sanitized := core.SanitizeClipFilename(outFn); sanitized != "" {
		outFn = sanitized
	}
	return enc, outMimeTry, outFn, true
}

// transcodeToPNG tries sips first (native, handles HEIC), then Go decode.
func transcodeToPNG(raw []byte) ([]byte, error) {
	if out, err := transcodeToPNGViaSips(raw); err == nil && len(out) > 0 {
		return out, nil
	}
	return transcodeToPNGViaGo(raw)
}

// transcodeToPNGViaSips uses sips (macOS) to convert raw image to PNG.
// Separate args per golang-security: never shell interpolation.
func transcodeToPNGViaSips(raw []byte) ([]byte, error) {
	in, err := os.CreateTemp("", "fuse-norm-in-*")
	if err != nil {
		return nil, fmt.Errorf("create temp in: %w", err)
	}
	inPath := in.Name()
	defer func() { _ = os.Remove(inPath) }()
	if _, err := in.Write(raw); err != nil {
		_ = in.Close()
		return nil, fmt.Errorf("write temp in: %w", err)
	}
	_ = in.Close()

	out, err := os.CreateTemp("", "fuse-norm-out-*.png")
	if err != nil {
		return nil, fmt.Errorf("create temp out: %w", err)
	}
	outPath := out.Name()
	_ = out.Close()
	defer func() { _ = os.Remove(outPath) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// sips -s format png <in> --out <out>  — args separate, no shell.
	cmd := exec.CommandContext(ctx, "sips", "-s", "format", "png", inPath, "--out", outPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sips: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, fmt.Errorf("read sips out: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("sips produced empty file")
	}
	return data, nil
}

// transcodeToPNGViaGo decodes via image.Decode (TIFF via extra import) and encodes PNG.
func transcodeToPNGViaGo(raw []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}
	return buf.Bytes(), nil
}

func encodeJPEG(pngRaw []byte, quality int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(pngRaw))
	if err != nil {
		return nil, fmt.Errorf("decode for jpeg: %w", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}
