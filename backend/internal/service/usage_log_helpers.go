package service

import "strings"

func optionalTrimmedStringPtr(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

// coalesceRequestedReasoningEffort prefers the client-requested value and falls
// back to the effective/forwarded effort for historical or unmapped rows.
func coalesceRequestedReasoningEffort(requested, forwarded *string) *string {
	if trimmed := optionalStringValue(requested); trimmed != "" {
		return &trimmed
	}
	if trimmed := optionalStringValue(forwarded); trimmed != "" {
		return &trimmed
	}
	return nil
}

func forwardResultBillingModel(requestedModel, upstreamModel string) string {
	if trimmed := strings.TrimSpace(requestedModel); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(upstreamModel)
}

// requestedBillingModelForConfiguredMapping returns the public/original model
// when the effective upstream route changed because of an operator-configured
// group/channel/account model mapping.
//
// It deliberately ignores UpstreamResponseModel: a provider-side response
// model change is audit data, not evidence that an admin mapping was applied.
func requestedBillingModelForConfiguredMapping(account *Account, fields ChannelUsageFields) (string, bool) {
	requested := strings.TrimSpace(fields.OriginalModel)
	if requested == "" {
		return "", false
	}

	routed := strings.TrimSpace(fields.ChannelMappedModel)
	if routed == "" {
		routed = requested
	}
	if !strings.EqualFold(routed, requested) {
		return requested, true
	}

	// OpenAI passthrough explicitly ignores stale account model_mapping.
	if account == nil || account.IsOpenAIPassthroughEnabled() {
		return "", false
	}
	mapped, matched := account.ResolveMappedModel(routed)
	mapped = strings.TrimSpace(mapped)
	if matched && mapped != "" && !strings.EqualFold(mapped, routed) {
		return requested, true
	}
	return "", false
}

func optionalInt64Ptr(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}
