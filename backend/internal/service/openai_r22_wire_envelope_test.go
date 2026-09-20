package service

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestValidateOpenAIResponsesFinalEnvelopeIdentity(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"synthetic"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)
	require.NoError(t, validateOpenAIResponsesFinalEnvelope(req, body))
}

func TestValidateOpenAIResponsesFinalEnvelopeZstd(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"synthetic"}`)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(3)))
	require.NoError(t, err)
	compressed := encoder.EncodeAll(body, nil)
	require.NoError(t, encoder.Close())

	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", bytes.NewReader(compressed))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "zstd")
	req.ContentLength = int64(len(compressed))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(compressed)), nil
	}
	require.NoError(t, validateOpenAIResponsesFinalEnvelope(req, body))
}

func TestValidateOpenAIResponsesFinalEnvelopeRejectsLengthMismatch(t *testing.T) {
	body := []byte(`{"input":"synthetic"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)
	req.ContentLength = int64(len(body) + 1)
	require.ErrorContains(t, validateOpenAIResponsesFinalEnvelope(req, body), "content length mismatch")
}

func TestValidateOpenAIResponsesFinalEnvelopeRejectsSemanticMismatch(t *testing.T) {
	wire := []byte(`{"input":"wire"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", bytes.NewReader(wire))
	require.NoError(t, err)
	require.ErrorContains(
		t,
		validateOpenAIResponsesFinalEnvelope(req, []byte(`{"input":"projected"}`)),
		"does not match",
	)
}

func TestValidateOpenAIResponsesFinalEnvelopeRejectsUnknownEncoding(t *testing.T) {
	body := []byte(`{"input":"synthetic"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "br")
	require.ErrorContains(t, validateOpenAIResponsesFinalEnvelope(req, body), "unsupported final Content-Encoding")
}
