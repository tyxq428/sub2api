package service

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

type openAIManualResponseModelAliasContextKey struct{}

type openAIManualResponseModelAlias struct {
	requestedModel string
	routedModel    string
}

// WithOpenAIManualResponseModelAlias records an operator-configured model
// alias that was applied before the request entered OpenAIGatewayService.
//
// The raw upstream model is still observed before any client-facing rewrite,
// so usage/billing/audit retain the real model that was sent/returned.
func WithOpenAIManualResponseModelAlias(ctx context.Context, requestedModel, routedModel string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	requestedModel = strings.TrimSpace(requestedModel)
	routedModel = strings.TrimSpace(routedModel)
	if requestedModel == "" || routedModel == "" || requestedModel == routedModel {
		return ctx
	}
	return context.WithValue(ctx, openAIManualResponseModelAliasContextKey{}, openAIManualResponseModelAlias{
		requestedModel: requestedModel,
		routedModel:    routedModel,
	})
}

func openAIManualResponseModelAliasFromContext(ctx context.Context) (openAIManualResponseModelAlias, bool) {
	if ctx == nil {
		return openAIManualResponseModelAlias{}, false
	}
	alias, ok := ctx.Value(openAIManualResponseModelAliasContextKey{}).(openAIManualResponseModelAlias)
	return alias, ok
}

const openAIResponseModelRestorePlanKey = "openai_response_model_restore_plan"

type openAIResponseModelRestorePlan struct {
	fromModel string
	toModel   string
}

// resetOpenAIResponseModelRestorePlan prevents a failed account attempt from
// leaking its response-alias plan into a later failover attempt on the same
// gin context.
func resetOpenAIResponseModelRestorePlan(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(openAIResponseModelRestorePlanKey, openAIResponseModelRestorePlan{})
}

// stageOpenAIResponseModelRestorePlan freezes the initial routed model for this
// account attempt. It deliberately does not follow later automatic fallbacks:
// if the upstream changes to another model, that real upstream downgrade stays
// visible to the client.
func stageOpenAIResponseModelRestorePlan(c *gin.Context, serviceOriginalModel, initialUpstreamModel string) {
	if c == nil {
		return
	}
	fromModel := strings.TrimSpace(initialUpstreamModel)
	toModel := strings.TrimSpace(serviceOriginalModel)
	if c.Request != nil {
		if alias, ok := openAIManualResponseModelAliasFromContext(c.Request.Context()); ok &&
			strings.EqualFold(strings.TrimSpace(alias.routedModel), toModel) {
			toModel = strings.TrimSpace(alias.requestedModel)
		}
	}
	c.Set(openAIResponseModelRestorePlanKey, openAIResponseModelRestorePlan{
		fromModel: fromModel,
		toModel:   toModel,
	})
}

func resolveOpenAIResponseModelRestorePlan(c *gin.Context, fallbackFromModel, fallbackToModel string) (fromModel, toModel string, ok bool) {
	if c != nil {
		if raw, exists := c.Get(openAIResponseModelRestorePlanKey); exists {
			plan, _ := raw.(openAIResponseModelRestorePlan)
			fromModel = strings.TrimSpace(plan.fromModel)
			toModel = strings.TrimSpace(plan.toModel)
			return fromModel, toModel, fromModel != "" && toModel != "" && fromModel != toModel
		}
	}
	fromModel = strings.TrimSpace(fallbackFromModel)
	toModel = strings.TrimSpace(fallbackToModel)
	return fromModel, toModel, fromModel != "" && toModel != "" && fromModel != toModel
}

func openAIClientFacingResponseModel(c *gin.Context, fallback string) string {
	if c != nil {
		if raw, exists := c.Get(openAIResponseModelRestorePlanKey); exists {
			if plan, ok := raw.(openAIResponseModelRestorePlan); ok {
				if model := strings.TrimSpace(plan.toModel); model != "" {
					return model
				}
			}
		}
	}
	return strings.TrimSpace(fallback)
}

// openAIClientFacingObservedModel preserves a genuine upstream-declared model
// change. Only an observed model that exactly equals the frozen manually
// routed model is restored to the public alias.
func openAIClientFacingObservedModel(c *gin.Context, observedModel, fallbackFromModel, fallbackToModel string) string {
	observedModel = strings.TrimSpace(observedModel)
	fromModel, toModel, ok := resolveOpenAIResponseModelRestorePlan(c, fallbackFromModel, fallbackToModel)
	if ok && observedModel != "" && observedModel == fromModel {
		return toModel
	}
	if observedModel != "" {
		return observedModel
	}
	if ok {
		return toModel
	}
	return strings.TrimSpace(fallbackToModel)
}
