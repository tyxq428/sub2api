package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	if account == nil || !account.IsCodexR1CanaryEnabled() || !account.UsesOpenAICodexProtocol() {
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

// validateOpenAIResponsesFinalEnvelope verifies the bytes that the transport
// is about to send without changing their semantics. It is intentionally
// separate from compression so R2.2 can prove that the encoded wire body
// decodes to the already projected semantic body.
func validateOpenAIResponsesFinalEnvelope(req *http.Request, semanticBody []byte) error {
	if req == nil || req.Body == nil {
		return errors.New("openai final envelope has no request body")
	}
	var (
		wire []byte
		err  error
	)
	if req.GetBody != nil {
		copyBody, getErr := req.GetBody()
		if getErr != nil {
			return fmt.Errorf("copy final request body: %w", getErr)
		}
		wire, err = io.ReadAll(copyBody)
		_ = copyBody.Close()
	} else {
		wire, err = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(wire))
	}
	if err != nil {
		return fmt.Errorf("read final request body: %w", err)
	}
	if req.ContentLength >= 0 && req.ContentLength != int64(len(wire)) {
		return fmt.Errorf("final content length mismatch: header=%d actual=%d", req.ContentLength, len(wire))
	}

	decoded := wire
	switch strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding"))) {
	case "", "identity":
	case "zstd":
		decoder, decodeErr := zstd.NewReader(nil)
		if decodeErr != nil {
			return fmt.Errorf("create final zstd decoder: %w", decodeErr)
		}
		defer decoder.Close()
		decoded, decodeErr = decoder.DecodeAll(wire, nil)
		if decodeErr != nil {
			return fmt.Errorf("decode final zstd body: %w", decodeErr)
		}
	default:
		return fmt.Errorf("unsupported final Content-Encoding %q", req.Header.Get("Content-Encoding"))
	}
	if !bytes.Equal(decoded, semanticBody) {
		return errors.New("final encoded request body does not match projected semantic body")
	}
	return nil
}
