package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"yuanbao2api/internal/models"
	providers "yuanbao2api/providers"
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