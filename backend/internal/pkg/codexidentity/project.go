package codexidentity

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	headerTurnMetadataPrefix = "header.x-codex-turn-metadata."
	bodyTurnMetadataPrefix   = "client_metadata.x-codex-turn-metadata."
)

func ProjectHTTP(headers http.Header, body []byte, plan OutboundPlan) (http.Header, []byte, error) {
	if err := plan.Validate(); err != nil {
		return nil, nil, err
	}
	nextHeaders := make(http.Header, len(headers))
	for name, values := range headers {
		nextHeaders[name] = append([]string(nil), values...)
	}
	nextBody := append([]byte(nil), body...)
	for _, patch := range plan.Patches {
		var err error
		switch patch.Carrier {
		case CarrierHeader:
			nextHeaders.Set(patch.Path, patch.Value)
		case CarrierBody:
			nextBody, err = sjson.SetBytes(nextBody, patch.Path, patch.Value)
		case CarrierTurnMetadata:
			switch {
			case strings.HasPrefix(patch.Path, headerTurnMetadataPrefix):
				err = patchHeaderTurnMetadata(nextHeaders, strings.TrimPrefix(patch.Path, headerTurnMetadataPrefix), patch.Value)
			case strings.HasPrefix(patch.Path, bodyTurnMetadataPrefix):
				nextBody, err = patchBodyTurnMetadata(nextBody, strings.TrimPrefix(patch.Path, bodyTurnMetadataPrefix), patch.Value)
			default:
				err = fmt.Errorf("unsupported turn metadata projection path %q", patch.Path)
			}
		default:
			err = fmt.Errorf("unsupported projection carrier %q", patch.Carrier)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return nextHeaders, nextBody, nil
}

func patchHeaderTurnMetadata(headers http.Header, path, value string) error {
	raw := strings.TrimSpace(headers.Get("x-codex-turn-metadata"))
	if raw == "" || !gjson.Valid(raw) {
		return fmt.Errorf("valid x-codex-turn-metadata header is required for %s", path)
	}
	next, err := sjson.Set(raw, path, value)
	if err != nil {
		return fmt.Errorf("patch x-codex-turn-metadata.%s: %w", path, err)
	}
	headers.Set("x-codex-turn-metadata", next)
	return nil
}

func patchBodyTurnMetadata(body []byte, path, value string) ([]byte, error) {
	embedded := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata")
	if embedded.Type != gjson.String || !gjson.Valid(embedded.String()) {
		return nil, fmt.Errorf("valid client_metadata.x-codex-turn-metadata is required for %s", path)
	}
	metadata, err := sjson.Set(embedded.String(), path, value)
	if err != nil {
		return nil, fmt.Errorf("patch embedded turn metadata %s: %w", path, err)
	}
	next, err := sjson.SetBytes(body, "client_metadata.x-codex-turn-metadata", metadata)
	if err != nil {
		return nil, fmt.Errorf("splice embedded turn metadata: %w", err)
	}
	return next, nil
}
