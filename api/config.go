package api

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"yuanbao2api/yuanbao"
)

// ServerConfig holds the dynamic server configuration (same as internal/config but for API layer)
var (
	serverConfig     = &ServerConfigData{DeepThinking: false, InternetSearch: false, DefaultModel: "deep_seek_v3"}
	serverConfigLock sync.RWMutex
)

// ServerConfigData represents the server configuration
type ServerConfigData struct {
	DeepThinking   bool   `json:"deepThinking"`
	InternetSearch bool   `json:"internetSearch"`
	DefaultModel   string `json:"defaultModel"`

	// Rate limiting (resolved from persisted overrides/env at startup; informational here).
	MaxConcurrency      int `json:"maxConcurrency"`
	QueueTimeoutSeconds int `json:"queueTimeoutSeconds"`
	RequestCooldownMs   int `json:"requestCooldownMs"`

	// AgentID — runtime-settable via /api/config
	AgentID string `json:"agentId"`

	// YuanbaoCookie — optional override for YUANBAO_COOKIE. nil means
	// "no runtime override" (fall back to env). A pointer whose struct has
	// both fields empty is treated the same as nil by EffectiveYuanbaoCookie:
	// HandleSetConfig collapses an all-empty object into nil so the field
	// never carries a meaningless zero-value pointer.
	YuanbaoCookie *YuanbaoCookie `json:"yuanbaoCookie"`

	// DefaultProvider is the registry's default provider name
	// (currently always "yuanbao"; the field is exposed so the
	// panel can render the current selection without an extra
	// fetch). Updated only via the new-shape /api/config body
	// `{defaultProvider: "..."}`.
	DefaultProvider string `json:"defaultProvider"`

	// Providers holds the per-provider configuration map. This is the
	// in-memory mirror of the on-disk RuntimeConfig.Providers; it is
	// kept here so the GET /api/config response can render the full
	// picture without an extra file read.
	Providers map[string]ProviderConfig `json:"providers,omitempty"`
}

// getEnvInt reads an integer environment variable, falling back to def on
// missing/invalid values.
func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// EffectiveYuanbaoCookie resolves the upstream Cookie that the yuanbao
// client should send on its next request. Priority:
//   1. ServerConfigData.YuanbaoCookie (runtime override, if non-nil and at
//      least one field is non-empty after assembly)
//   2. YUANBAO_COOKIE environment variable (if non-empty)
//   3. "" (caller treats as "no cookie")
//
// This is the single resolution entry point; upstream callers (yuanbao/client.go,
// HandleEnv) MUST go through this function rather than reading env directly.
func EffectiveYuanbaoCookie() string {
	serverConfigLock.RLock()
	yc := serverConfig.YuanbaoCookie
	serverConfigLock.RUnlock()
	if yc != nil {
		if h := yc.HeaderValue(); h != "" {
			return h
		}
	}
	return os.Getenv("YUANBAO_COOKIE")
}

// YuanbaoCookieSource describes where the currently-effective Cookie came from.
// It mirrors the cookieSource field exposed via /api/env.
type YuanbaoCookieSource string

const (
	CookieSourceRuntime YuanbaoCookieSource = "runtime"
	CookieSourceEnv     YuanbaoCookieSource = "env"
	CookieSourceNone    YuanbaoCookieSource = "none"
)

// EffectiveYuanbaoCookieSource reports whether the effective Cookie came from
// the runtime override or the env var. Callers must call
// EffectiveYuanbaoCookie first; if it returns "", this returns "none".
func EffectiveYuanbaoCookieSource() YuanbaoCookieSource {
	serverConfigLock.RLock()
	yc := serverConfig.YuanbaoCookie
	serverConfigLock.RUnlock()
	if yc != nil && (yc.HyToken != "" || yc.HyUser != "") {
		return CookieSourceRuntime
	}
	if v := os.Getenv("YUANBAO_COOKIE"); v != "" {
		return CookieSourceEnv
	}
	return CookieSourceNone
}

// init wires the yuanbao client's CookieResolver to this package's
// EffectiveYuanbaoCookie. This must run after yuanbao's package init
// (which sets the default to a no-op). Go's import order guarantees
// that: api imports yuanbao, so yuanbao's init runs first.
//
// We cannot call EffectiveYuanbaoCookie directly from the yuanbao
// package because that would create an import cycle (api already
// imports yuanbao for NewClient). The function pointer indirection
// keeps the resolution logic in one place (here) while letting the
// client stay cycle-free.
func init() {
	yuanbao.CookieResolver = EffectiveYuanbaoCookie
}

// HandleStatus returns live concurrency/queue stats for observability.
func HandleStatus(c *gin.Context) {
	rl := GetRateLimiter()
	if rl == nil {
		c.JSON(http.StatusOK, gin.H{
			"maxConcurrency":      0,
			"queueTimeoutSeconds": 0,
			"requestCooldownMs":   0,
			"inflight":            0,
			"waiting":             0,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"maxConcurrency":      rl.MaxConcurrency(),
		"queueTimeoutSeconds": int(rl.QueueTimeout().Seconds()),
		"requestCooldownMs":   int(rl.Cooldown().Milliseconds()),
		"inflight":            rl.Inflight(),
		"waiting":             rl.Waiting(),
	})
}

// HandleGetConfig returns the current server configuration
func HandleGetConfig(c *gin.Context) {
	serverConfigLock.RLock()
	defer serverConfigLock.RUnlock()
	c.JSON(http.StatusOK, serverConfig)
}

// HandleSetConfig updates the server configuration. The request body is
// read as a raw map so that omitted fields do NOT zero-out existing
// values (avoids the Go zero-value pitfall when binding partial updates
// into a struct).
//
// The handler accepts three mutually-exclusive body shapes:
//
//  1. New form: {providers: {<name>: {...}}, defaultProvider: "..."}.
//     Each provider block may carry enabled, cookie, agentId,
//     maxConcurrency, queueTimeoutSeconds, requestCooldownMs.
//  2. Legacy form: {yuanbaoCookie: {...}, maxConcurrency: N, ...}
//     without a `providers` key. Translated to Providers["yuanbao"]
//     before persistence.
//  3. Flat form: {deepThinking, internetSearch, defaultModel,
//     defaultProvider}. Used by the panel's feature toggles and
//     default-model selector; does not touch providers[] at all.
func HandleSetConfig(c *gin.Context) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1) New shape: providers key present.
	if _, hasProviders := body["providers"]; hasProviders {
		handleSetConfigProviders(c, body)
		return
	}

	// 2) Legacy shape: any of yuanbaoCookie / maxConcurrency /
	//    queueTimeoutSeconds / requestCooldownMs present without a
	//    providers key.
	if isLegacyForm(body) {
		handleSetConfigLegacy(c, body)
		return
	}

	// 3) Flat shape (or empty body): the original partial-update
	//    behavior for deepThinking / internetSearch / defaultModel /
	//    agentId / defaultProvider stays intact.
	handleSetConfigFlat(c, body)
}

// isLegacyForm reports whether the request body contains any of the
// legacy flat keys and no providers key. The check is intentionally
// lenient (any legacy key wins) so an empty body falls through to the
// flat-shape branch.
func isLegacyForm(body map[string]interface{}) bool {
	for _, k := range []string{"yuanbaoCookie", "maxConcurrency", "queueTimeoutSeconds", "requestCooldownMs", "agentId"} {
		if _, ok := body[k]; ok {
			return true
		}
	}
	return false
}

// handleSetConfigProviders applies the new-shape request body. Every
// recognized field inside a provider block is validated; unknown
// fields cause a 400 so future additions stay explicit.
func handleSetConfigProviders(c *gin.Context, body map[string]interface{}) {
	rawProviders, ok := body["providers"].(map[string]interface{})
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "providers must be an object"})
		return
	}

	// Build a ProviderConfig for each entry (no provider mutates
	// shared state yet; we apply under the global lock below).
	type pendingProvider struct {
		name   string
		config ProviderConfig
	}
	var pending []pendingProvider

	for name, rawCfg := range rawProviders {
		obj, ok := rawCfg.(map[string]interface{})
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "providers." + name + " must be an object"})
			return
		}
		cfg, errMsg := parseProviderConfig(name, obj)
		if errMsg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
		pending = append(pending, pendingProvider{name: name, config: cfg})
	}

	// Determine the new defaultProvider (only honoured if explicitly
	// supplied in this request; absent keeps the current default).
	defaultProvider := ""
	if v, ok := body["defaultProvider"]; ok {
		s, isString := v.(string)
		if !isString {
			c.JSON(http.StatusBadRequest, gin.H{"error": "defaultProvider must be a string"})
			return
		}
		defaultProvider = s
		if defaultProvider != "" {
			if _, exists := rawProviders[defaultProvider]; !exists {
				c.JSON(http.StatusBadRequest, gin.H{"error": "defaultProvider '" + defaultProvider + "' is not in providers"})
				return
			}
		}
	}

	// Apply under the global lock.
	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()

	if serverConfig.Providers == nil {
		serverConfig.Providers = map[string]ProviderConfig{}
	}

	rateLimitChanged := false
	cookieChanged := false

	for _, p := range pending {
		prev := serverConfig.Providers[p.name]
		next := mergeProviderConfig(prev, p.config)

		// Rate-limit change detection: any of the three rate-limit
		// knobs changed for the yuanbao provider (the only one whose
		// limiter exists today) triggers a restart.
		if p.name == "yuanbao" {
			if !intPtrEq(prev.MaxConcurrency, next.MaxConcurrency) ||
				!intPtrEq(prev.QueueTimeoutSeconds, next.QueueTimeoutSeconds) ||
				!intPtrEq(prev.RequestCooldownMs, next.RequestCooldownMs) {
				rateLimitChanged = true
				// Mirror the resolved values into serverConfig so the
				// informational fields stay in sync.
				if next.MaxConcurrency != nil {
					serverConfig.MaxConcurrency = *next.MaxConcurrency
				}
				if next.QueueTimeoutSeconds != nil {
					serverConfig.QueueTimeoutSeconds = *next.QueueTimeoutSeconds
				}
				if next.RequestCooldownMs != nil {
					serverConfig.RequestCooldownMs = *next.RequestCooldownMs
				}
			}
		}

		// Cookie change detection for the hot-path (no restart).
		if p.name == "yuanbao" && !cookiePtrEq(prev.Cookie, next.Cookie) {
			cookieChanged = true
			serverConfig.YuanbaoCookie = next.Cookie
		}

		if p.name == "yuanbao" && next.AgentID != nil {
			serverConfig.AgentID = *next.AgentID
		}

		serverConfig.Providers[p.name] = next
	}

	if defaultProvider != "" {
		serverConfig.DefaultProvider = defaultProvider
	}

	// Also handle flat defaultProvider (top-level) which may be sent
	// without any providers block.
	if v, ok := body["defaultProvider"]; ok {
		if s, ok := v.(string); ok && s != "" {
			serverConfig.DefaultProvider = s
		}
	}

	// Flat fields on the same request — accept the same partial
	// updates the old handler accepted.
	applyFlatFields(body, serverConfig)

	// Persist.
	if err := saveServerConfigAsRuntime(serverConfig); err != nil {
		log.Printf("保存运行时配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist runtime config: " + err.Error()})
		return
	}

	if rateLimitChanged {
		log.Println("检测到限流参数变更，500ms 后自动重启以使新配置生效")
		go func() {
			time.Sleep(500 * time.Millisecond)
			exitFn(0)
		}()
	} else if cookieChanged {
		log.Println("Cookie 已更新，立即生效（无需重启）")
	}

	c.JSON(http.StatusOK, serverConfig)
}

// parseProviderConfig validates and converts a raw provider config
// object into a typed ProviderConfig. On any validation failure it
// returns a non-empty error message.
func parseProviderConfig(name string, obj map[string]interface{}) (ProviderConfig, string) {
	var cfg ProviderConfig
	for k := range obj {
		switch k {
		case "enabled", "cookie", "agentId",
			"maxConcurrency", "queueTimeoutSeconds", "requestCooldownMs":
			// recognized
		default:
			return cfg, "providers." + name + " has unknown field: " + k
		}
	}
	if v, ok := obj["enabled"]; ok {
		b, isBool := v.(bool)
		if !isBool {
			return cfg, "providers." + name + ".enabled must be a boolean"
		}
		cfg.Enabled = &b
	}
	if v, ok := obj["cookie"]; ok {
		yc, errMsg := parseYuanbaoCookieValue(v)
		if errMsg != "" {
			return cfg, "providers." + name + ".cookie: " + errMsg
		}
		cfg.Cookie = yc
	}
	if v, ok := obj["agentId"]; ok {
		s, isString := v.(string)
		if !isString {
			return cfg, "providers." + name + ".agentId must be a string"
		}
		cfg.AgentID = &s
	}
	if v, ok := obj["maxConcurrency"]; ok {
		n, valid := toInt(v)
		if !valid || n < 1 || n > 1000 {
			return cfg, "providers." + name + ".maxConcurrency out of range (1..1000)"
		}
		cfg.MaxConcurrency = &n
	}
	if v, ok := obj["queueTimeoutSeconds"]; ok {
		n, valid := toInt(v)
		if !valid || n < 1 || n > 3600 {
			return cfg, "providers." + name + ".queueTimeoutSeconds out of range (1..3600)"
		}
		cfg.QueueTimeoutSeconds = &n
	}
	if v, ok := obj["requestCooldownMs"]; ok {
		n, valid := toInt(v)
		if !valid || n < 0 || n > 60000 {
			return cfg, "providers." + name + ".requestCooldownMs out of range (0..60000)"
		}
		cfg.RequestCooldownMs = &n
	}
	return cfg, ""
}

// parseYuanbaoCookieValue accepts an object {hyToken, hyUser} or
// null. Strings are rejected (legacy string form is migrated
// upstream; the panel should always send the object form).
func parseYuanbaoCookieValue(v interface{}) (*YuanbaoCookie, string) {
	if v == nil {
		return nil, ""
	}
	obj, isObject := v.(map[string]interface{})
	if !isObject {
		return nil, "must be an object"
	}
	for k := range obj {
		if k != "hyToken" && k != "hyUser" {
			return nil, "has unknown field: " + k
		}
	}
	var hyToken, hyUser string
	if raw, has := obj["hyToken"]; has {
		s, isString := raw.(string)
		if !isString {
			return nil, "hyToken must be a string"
		}
		hyToken = s
	}
	if raw, has := obj["hyUser"]; has {
		s, isString := raw.(string)
		if !isString {
			return nil, "hyUser must be a string"
		}
		hyUser = s
	}
	if hyToken == "" && hyUser == "" {
		// explicit clear: produce a non-nil pointer with both fields
		// empty so the merge step knows the user requested a clear
		// (vs. the key being absent).
		yc := YuanbaoCookie{}
		return &yc, ""
	}
	return &YuanbaoCookie{HyToken: hyToken, HyUser: hyUser}, ""
}

// mergeProviderConfig overlays the new (request-supplied) ProviderConfig
// on top of the previous one. A nil pointer in `next` means "do not
// touch this field" — only non-nil pointers (including explicit clears
// where the pointer is non-nil but the underlying value is empty) win.
func mergeProviderConfig(prev, next ProviderConfig) ProviderConfig {
	out := prev
	if next.Enabled != nil {
		out.Enabled = next.Enabled
	}
	if next.Cookie != nil {
		out.Cookie = next.Cookie
	}
	if next.AgentID != nil {
		out.AgentID = next.AgentID
	}
	if next.MaxConcurrency != nil {
		out.MaxConcurrency = next.MaxConcurrency
	}
	if next.QueueTimeoutSeconds != nil {
		out.QueueTimeoutSeconds = next.QueueTimeoutSeconds
	}
	if next.RequestCooldownMs != nil {
		out.RequestCooldownMs = next.RequestCooldownMs
	}
	return out
}

func intPtrEq(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func cookiePtrEq(a, b *YuanbaoCookie) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// handleSetConfigLegacy translates the legacy {yuanbaoCookie, ...}
// flat body to Providers["yuanbao"], then persists in the new shape.
func handleSetConfigLegacy(c *gin.Context, body map[string]interface{}) {
	// Extract the legacy fields first (no mutation yet).
	var maxConcurrency, queueTimeoutSeconds, requestCooldownMs *int
	if v, ok := body["maxConcurrency"]; ok {
		n, valid := toInt(v)
		if !valid || n < 1 || n > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "maxConcurrency out of range (1..1000)"})
			return
		}
		maxConcurrency = &n
	}
	if v, ok := body["queueTimeoutSeconds"]; ok {
		n, valid := toInt(v)
		if !valid || n < 1 || n > 3600 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "queueTimeoutSeconds out of range (1..3600)"})
			return
		}
		queueTimeoutSeconds = &n
	}
	if v, ok := body["requestCooldownMs"]; ok {
		n, valid := toInt(v)
		if !valid || n < 0 || n > 60000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "requestCooldownMs out of range (0..60000)"})
			return
		}
		requestCooldownMs = &n
	}

	var yuanbaoCookieSet bool
	var yuanbaoCookieValue *YuanbaoCookie
	if v, ok := body["yuanbaoCookie"]; ok {
		obj, isObject := v.(map[string]interface{})
		if !isObject {
			c.JSON(http.StatusBadRequest, gin.H{"error": "yuanbaoCookie must be an object"})
			return
		}
		var (
			hyToken string
			hyUser  string
		)
		if raw, has := obj["hyToken"]; has {
			s, isString := raw.(string)
			if !isString {
				c.JSON(http.StatusBadRequest, gin.H{"error": "yuanbaoCookie.hyToken must be a string"})
				return
			}
			hyToken = s
		}
		if raw, has := obj["hyUser"]; has {
			s, isString := raw.(string)
			if !isString {
				c.JSON(http.StatusBadRequest, gin.H{"error": "yuanbaoCookie.hyUser must be a string"})
				return
			}
			hyUser = s
		}
		for k := range obj {
			if k != "hyToken" && k != "hyUser" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "yuanbaoCookie has unknown field: " + k})
				return
			}
		}
		yuanbaoCookieSet = true
		if hyToken != "" || hyUser != "" {
			yc := YuanbaoCookie{HyToken: hyToken, HyUser: hyUser}
			yuanbaoCookieValue = &yc
		}
	}

	var agentIDValue *string
	if v, ok := body["agentId"]; ok {
		s, isString := v.(string)
		if !isString {
			c.JSON(http.StatusBadRequest, gin.H{"error": "agentId must be a string"})
			return
		}
		if s != "" {
			agentIDValue = &s
		}
	}

	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()

	if serverConfig.Providers == nil {
		serverConfig.Providers = map[string]ProviderConfig{}
	}
	yuanbao := serverConfig.Providers["yuanbao"]
	prevMaxC, prevQT, prevCD := yuanbao.MaxConcurrency, yuanbao.QueueTimeoutSeconds, yuanbao.RequestCooldownMs
	enabled := true
	yuanbao.Enabled = &enabled

	rateLimitChanged := false
	if maxConcurrency != nil {
		yuanbao.MaxConcurrency = maxConcurrency
		serverConfig.MaxConcurrency = *maxConcurrency
		rateLimitChanged = true
	}
	if queueTimeoutSeconds != nil {
		yuanbao.QueueTimeoutSeconds = queueTimeoutSeconds
		serverConfig.QueueTimeoutSeconds = *queueTimeoutSeconds
		rateLimitChanged = true
	}
	if requestCooldownMs != nil {
		yuanbao.RequestCooldownMs = requestCooldownMs
		serverConfig.RequestCooldownMs = *requestCooldownMs
		rateLimitChanged = true
	}
	if yuanbaoCookieSet {
		yuanbao.Cookie = yuanbaoCookieValue
		serverConfig.YuanbaoCookie = yuanbaoCookieValue
	}
	if agentIDValue != nil {
		yuanbao.AgentID = agentIDValue
		serverConfig.AgentID = *agentIDValue
	}
	serverConfig.Providers["yuanbao"] = yuanbao
	if serverConfig.DefaultProvider == "" {
		serverConfig.DefaultProvider = "yuanbao"
	}
	_ = prevMaxC
	_ = prevQT
	_ = prevCD

	applyFlatFields(body, serverConfig)

	if err := saveServerConfigAsRuntime(serverConfig); err != nil {
		log.Printf("保存运行时配置失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist runtime config: " + err.Error()})
		return
	}

	if rateLimitChanged {
		log.Println("检测到限流参数变更，500ms 后自动重启以使新配置生效")
		go func() {
			time.Sleep(500 * time.Millisecond)
			exitFn(0)
		}()
	} else if yuanbaoCookieSet {
		log.Println("Cookie 已更新，立即生效（无需重启）")
	}

	c.JSON(http.StatusOK, serverConfig)
}

// handleSetConfigFlat preserves the original behavior for partial
// updates to deepThinking / internetSearch / defaultModel /
// defaultProvider / agentId when no providers[] block is present.
func handleSetConfigFlat(c *gin.Context, body map[string]interface{}) {
	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()

	applyFlatFields(body, serverConfig)

	// No provider fields were touched, so no persistence or restart
	// is needed (mirrors the pre-change behavior).
	c.JSON(http.StatusOK, serverConfig)
}

// applyFlatFields updates the flat (non-provider) fields of
// serverConfig from the request body. Called by every branch.
func applyFlatFields(body map[string]interface{}, sc *ServerConfigData) {
	if v, ok := body["deepThinking"]; ok {
		if b, ok := v.(bool); ok {
			sc.DeepThinking = b
		}
	}
	if v, ok := body["internetSearch"]; ok {
		if b, ok := v.(bool); ok {
			sc.InternetSearch = b
		}
	}
	if v, ok := body["defaultModel"]; ok {
		if s, ok := v.(string); ok && s != "" {
			sc.DefaultModel = s
		}
	}
	if v, ok := body["defaultProvider"]; ok {
		if s, ok := v.(string); ok && s != "" {
			sc.DefaultProvider = s
		}
	}
}

// saveServerConfigAsRuntime persists the current serverConfig as a
// RuntimeConfig. Defaults are filled in for any missing pointer so the
// on-disk file always round-trips through the new shape.
func saveServerConfigAsRuntime(sc *ServerConfigData) error {
	providers := map[string]ProviderConfig{}
	for name, p := range sc.Providers {
		providers[name] = p
	}
	if entry, ok := providers["yuanbao"]; ok {
		// Ensure enabled is always set when an entry exists so old
		// reads still resolve the flag correctly.
		if entry.Enabled == nil {
			enabled := true
			entry.Enabled = &enabled
			providers["yuanbao"] = entry
		}
	}
	if len(providers) == 0 {
		// Synthesize a minimum entry so an empty update still
		// persists the current cookie + concurrency fields instead
		// of dropping them.
		maxC := sc.MaxConcurrency
		qTimeout := sc.QueueTimeoutSeconds
		cooldown := sc.RequestCooldownMs
		enabled := true
		providers["yuanbao"] = ProviderConfig{
			Enabled:             &enabled,
			Cookie:              sc.YuanbaoCookie,
			MaxConcurrency:      &maxC,
			QueueTimeoutSeconds: &qTimeout,
			RequestCooldownMs:   &cooldown,
		}
	}
	defaultProvider := sc.DefaultProvider
	if defaultProvider == "" {
		defaultProvider = "yuanbao"
	}
	cfg := RuntimeConfig{
		Providers:       providers,
		DefaultProvider: defaultProvider,
	}
	return SaveRuntimeConfig(cfg)
}

// toInt safely extracts an integer from a JSON-decoded value (numbers come
// back as float64 by default in Go's json package).
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		converted := int(n)
		if float64(converted) != n {
			return 0, false
		}
		return converted, true
	case int:
		return n, true
	default:
		return 0, false
	}
}

// SyncAgentID copies the env YUANBAO_AGENT_ID into serverConfig if not set.
func SyncAgentID() {
	serverConfigLock.Lock()
	defer serverConfigLock.Unlock()
	if serverConfig.AgentID == "" {
		if v := os.Getenv("YUANBAO_AGENT_ID"); v != "" {
			serverConfig.AgentID = v
		}
	}
}

// GetServerConfig returns a copy of the current server config
func GetServerConfig() ServerConfigData {
	serverConfigLock.RLock()
	defer serverConfigLock.RUnlock()
	return *serverConfig
}
