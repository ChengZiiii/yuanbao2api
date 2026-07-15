package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"yuanbao2api/internal/models"
	providers "yuanbao2api/providers"
	"yuanbao2api/yuanbao"
)

// enabled reports whether the given provider has enabled=true in the
// persisted runtime config. Defaults to true when the entry is absent
// (legacy single-provider deployments) so the panel does not see an
// empty model list after upgrading.
func providerEnabled(name string) bool {
	serverConfigLock.RLock()
	defer serverConfigLock.RUnlock()
	if serverConfig.Providers == nil {
		return true
	}
	p, ok := serverConfig.Providers[name]
	if !ok {
		return true
	}
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// HandleOpenAIModels returns the union of every enabled provider's
// Models() output. Each provider contributes its own ModelInfo list
// (already wrapped in the OpenAI-compatible shape by the provider
// adapter); the handler adds the standard object/created/permission/
// root/parent fields and the per-model owned_by = provider name.
func HandleOpenAIModels(c *gin.Context) {
	var entries []models.ModelInfo

	if reg := activeRegistry; reg != nil {
		for _, p := range reg.All() {
			if !providerEnabled(p.Name()) {
				continue
			}
			for _, m := range p.Models() {
				entries = append(entries, models.ModelInfo{
					ID:          m.ID,
					Object:      "model",
					Created:     1704067200,
					OwnedBy:     p.Name(),
					Permission:  []interface{}{},
					Root:        m.ID,
					Parent:      nil,
					Description: m.Description,
				})
			}
		}
	}

	if entries == nil {
		// Fall back to a minimal yuanbao-only list when no registry
		// is wired (e.g. unit tests before main.go boots) so the
		// endpoint still returns the canonical OpenAI shape.
		entries = []models.ModelInfo{
			{
				ID:          "deep_seek_v3",
				Object:      "model",
				Created:     1704067200,
				OwnedBy:     "yuanbao",
				Permission:  []interface{}{},
				Root:        "deep_seek_v3",
				Parent:      nil,
				Description: "DeepSeek - 适合深度思考和复杂推理任务",
			},
			{
				ID:          "hunyuan",
				Object:      "model",
				Created:     1704067200,
				OwnedBy:     "yuanbao",
				Permission:  []interface{}{},
				Root:        "hunyuan_gpt_175B_0404",
				Parent:      nil,
				Description: "Hy3 preview - 腾讯混元大模型，全能处理",
			},
		}
	}

	c.JSON(http.StatusOK, models.ModelsResponse{
		Object: "list",
		Data:   entries,
	})
}

// activeRegistry is the registry singleton exposed by main.go via
// SetProviderRegistry. When nil, HandleOpenAIModels falls back to
// the hard-coded yuanbao list above.
var activeRegistry *providers.Registry

// SetProviderRegistry wires the live provider.Registry into the
// handler. Called once from main.go at startup. Passing nil resets
// the handler to its static fallback.
func SetProviderRegistry(reg *providers.Registry) {
	activeRegistry = reg
}

// ---- Backwards-compat shims for the legacy handler code path. ----
// These remain in place until section 10 swaps openai.go / anthropic.go
// to use the provider.Registry directly. They are NOT used by the new
// HandleOpenAIModels implementation above.

// ModelConfig holds the mapping configuration for a model (legacy
// shim). The yuanbao provider now owns this knowledge internally.
type ModelConfig struct {
	ChatModelID string
	Model       string
	Name        string
	Description string
}

// MODEL_MAPPING mirrors the old yuanbao-only model map. Kept so the
// legacy GetModelConfig / buildYuanbaoRequest paths continue to
// resolve the same chatModelID values.
var MODEL_MAPPING = map[string]ModelConfig{
	"DeepSeek-V3.2": {
		ChatModelID: "deep_seek_v3",
		Model:       "gpt_175B_0404",
		Name:        "DeepSeek V3.2",
		Description: "适合深度思考和复杂推理任务",
	},
	"deep_seek_v3": {
		ChatModelID: "deep_seek_v3",
		Model:       "gpt_175B_0404",
		Name:        "DeepSeek V3.2",
		Description: "适合深度思考和复杂推理任务",
	},
	"deepseek": {
		ChatModelID: "deep_seek_v3",
		Model:       "gpt_175B_0404",
		Name:        "DeepSeek V3.2",
		Description: "适合深度思考和复杂推理任务",
	},
	"hunyuan-t1": {
		ChatModelID: "hunyuan_gpt_175B_0404",
		Model:       "gpt_175B_0404",
		Name:        "Hy3 preview",
		Description: "腾讯混元大模型，全能处理",
	},
	"hunyuan": {
		ChatModelID: "hunyuan_gpt_175B_0404",
		Model:       "gpt_175B_0404",
		Name:        "Hy3 preview",
		Description: "腾讯混元大模型，全能处理",
	},
	"gpt_175B_0404": {
		ChatModelID: "deep_seek_v3",
		Model:       "gpt_175B_0404",
		Name:        "GPT 175B",
		Description: "元宝内部模型",
	},
}

// GetModelConfig returns the model configuration for a given model name.
func GetModelConfig(modelName string) ModelConfig {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(modelName, "-", ""), "_", ""))

	if config, ok := MODEL_MAPPING[modelName]; ok {
		return config
	}

	for key, config := range MODEL_MAPPING {
		keyNormalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", ""), "_", ""))
		if keyNormalized == normalized {
			return config
		}
	}

	return MODEL_MAPPING["DeepSeek-V3.2"]
}

// buildYuanbaoRequest builds the Yuanbao API request from parameters.
// Retained as a back-compat shim for the legacy handler code path;
// the new handler code (section 10) calls providers/yuanbao.BuildRequest
// directly.
func buildYuanbaoRequest(prompt string, modelConfig ModelConfig, useDeepThinking bool, useInternetSearch bool, agentID string) yuanbao.YuanbaoRequest {
	supportFunctions := []string{}
	if useInternetSearch {
		supportFunctions = append(supportFunctions, "openInternetSearch")
	} else {
		supportFunctions = append(supportFunctions, "closeInternetSearch")
	}

	chatModelID := modelConfig.ChatModelID
	modelID := modelConfig.ChatModelID
	subModelID := ""

	if useDeepThinking {
		if strings.Contains(modelConfig.ChatModelID, "hunyuan") {
			chatModelID = "hunyuan_t1"
			modelID = "hunyuan_t1"
			subModelID = "hunyuan_t1"
		} else {
			chatModelID = "deep_seek"
			modelID = "deep_seek_v3"
			subModelID = "deep_seek"
		}
	}

	displayPrompt := prompt
	if len(prompt) > 2000 {
		displayPrompt = prompt[:2000] + "...[已截断]"
	}

	plugin := "Adaptive"
	if useDeepThinking {
		plugin = ""
	}

	internetSearchFn := "closeInternetSearch"
	if useInternetSearch {
		internetSearchFn = "openInternetSearch"
	}

	extInfo, _ := json.Marshal(map[string]interface{}{
		"modelId":    modelID,
		"subModelId": subModelID,
		"supportFunctions": map[string]string{
			"internetSearch": internetSearchFn,
		},
	})

	return yuanbao.YuanbaoRequest{
		Model:             modelConfig.Model,
		Prompt:            prompt,
		Plugin:            plugin,
		DisplayPrompt:     displayPrompt,
		DisplayPromptType: 1,
		AgentID:           agentID,
		IsTemporary:       true,
		ProjectID:         "",
		ChatModelID:       chatModelID,
		SupportFunctions:  supportFunctions,
		DocOpenID:         "",
		Options: map[string]interface{}{
			"imageIntention": map[string]interface{}{
				"needIntentionModel": true,
				"backendUpdateFlag":  2,
				"intentionStatus":    true,
			},
		},
		Multimedia:        []interface{}{},
		SupportHint:       1,
		ChatModelExtInfo:  string(extInfo),
		ApplicationIDList: []string{},
		Version:           "v2",
		ExtReportParams:   nil,
		IsAtomInput:       false,
		OffsetOfHour:      8,
		OffsetOfMinute:    0,
	}
}

// YuanbaoRequest aliases the yuanbao package's YuanbaoRequest type.
type YuanbaoRequest = yuanbao.YuanbaoRequest