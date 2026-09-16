package service

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

var (
	openAIResponsesZstdOnce    sync.Once
	openAIResponsesZstdEncoder *zstd.Encoder
	openAIResponsesZstdErr     error
)

func shouldCompressOpenAIResponsesRequest(c *gin.Context, account *Account) bool {
	if account == nil || !account.IsOpenAI() || !account.UsesOpenAICodexProtocol() {
		return false
	}
	// This batch intentionally mirrors only the base HTTP /responses request
	// for which the locked Codex reference has direct compression evidence.
	// /compact and /input_tokens stay unchanged until separately verified.
	return openAIResponsesRequestPathSuffix(c) == ""
}

func openAIResponsesZstd() (*zstd.Encoder, error) {
	openAIResponsesZstdOnce.Do(func() {
		openAIResponsesZstdEncoder, openAIResponsesZstdErr = zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)),
		)
	})
	if openAIResponsesZstdErr != nil {
		return nil, openAIResponsesZstdErr
	}
	if openAIResponsesZstdEncoder == nil {
		return nil, errors.New("openai responses zstd encoder is unavailable")
	}
	return openAIResponsesZstdEncoder, nil
}

func applyOpenAIResponsesRequestCompression(c *gin.Context, account *Account, req *http.Request, body []byte) error {
	if req == nil || !shouldCompressOpenAIResponsesRequest(c, account) {
		return nil
	}
	encoder, err := openAIResponsesZstd()
	if err != nil {
		return err
	}
	compressed := encoder.EncodeAll(body, nil)
	req.Body = io.NopCloser(bytes.NewReader(compressed))
	req.ContentLength = int64(len(compressed))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(compressed)), nil
	}
	req.Header.Set("Content-Encoding", "zstd")
	return nil
}
