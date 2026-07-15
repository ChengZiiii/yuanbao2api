package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"yuanbao2api/internal/models"
	"yuanbao2api/internal/utils"
	providers "yuanbao2api/providers"
	"yuanbao2api/session"
	"yuanbao2api/toolcall"
)

// getAgentID returns the Yuanbao agent ID from runtime config, env, or default.
func getAgentID() string {
	cfg := GetServerConfig()
	if cfg.AgentID != "" {
		return cfg.AgentID
	}
	agentID := os.Getenv("YUANBAO_AGENT_ID")
	if agentID == "" {
		agentID = "naQivTmsDa"
	}
	return agentID
}

// safeFlush attempts to flush the response writer, recovering from any panic
func safeFlush(w gin.ResponseWriter) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Flush failed (connection likely closed): %v", r)
		}
	}()
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// HandleOpenAIChatCompletion processes OpenAI-compatible chat completion
// requests. The model name is routed through provider.Registry so the
// request can fan out to any registered provider (yuanbao today;
// qwen/kimi once their real adapters land).
func HandleOpenAIChatCompletion(c *gin.Context) {
	var req models.OpenAIChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Received OpenAI chat completion request: model=%s, stream=%v", req.Model, req.Stream)

	cfg := GetServerConfig()

	// Apply global defaults if not specified
	model := req.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	useDeepThinking := req.DeepThinking
	if !req.DeepThinking {
		useDeepThinking = cfg.DeepThinking
	}
	useInternetSearch := req.InternetSearch
	if !req.InternetSearch {
		useInternetSearch = cfg.InternetSearch
	}

	// Route the model name to a provider. Unknown model → 400,
	// provider not implemented (placeholder) → 501.
	reg := activeRegistry
	if reg == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no provider registry configured"})
		return
	}
	prov, err := reg.Route(model)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Spec scenario "命中但 provider 停用": if the matching provider
	// is explicitly disabled in RuntimeConfig.Providers, refuse the
	// request. We do this here (rather than inside Registry.Route) so
	// /v1/models can still enumerate the provider's model list while
	// the chat endpoints reject calls to it.
	if !providerEnabled(prov.Name()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "provider disabled: " + prov.Name()})
		return
	}

	// Per-provider concurrency gate. Held for the entire critical
	// section (upstream call + stream/non-stream response writing)
	// and released via defer. Excess requests block in FIFO order
	// until a slot frees, up to the queue timeout.
	rl := GetLimiterManager().For(prov.Name())
	if err := rl.Acquire(c.Request.Context()); err != nil {
		log.Printf("Rate limit: rejecting OpenAI request (queue full/timeout) on %s: %v", prov.Name(), err)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": "并发已达上限，请求在队列中等待超时，请稍后重试。(" + err.Error() + ")",
				"type":    "rate_limit_error",
			},
		})
		return
	}
	defer rl.Release()

	// Build prompt via the provider adapter so each upstream sees the
	// framing it expects.
	promptMessages := convertToProviderMessages(req.Messages)
	prompt, toolSystemPrompt := utils.ConvertMessagesToYuanbaoPrompt(req.Messages, req.Tools)
	prompt = utils.TruncatePrompt(prompt, req.Messages, toolSystemPrompt)

	agentID := getAgentID()
	conversationID := session.GenerateConversationID()

	providerReq, err := prov.NewRequest(prompt, providers.RequestOptions{
		Model:           model,
		UseDeepThinking: useDeepThinking,
		UseInternet:     useInternetSearch,
		AgentID:         agentID,
	})
	if err != nil {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": fmt.Sprintf("provider %s: %v", prov.Name(), err),
		})
		return
	}

	resp, err := prov.Send(providerReq, agentID, conversationID)
	if err != nil {
		log.Printf("Error sending request to %s: %v", prov.Name(), err)
		// Placeholder providers (qwen/kimi) return a sentinel
		// ErrNotImplemented — surface that as 501 so the panel can
		// show a clear "not implemented yet" message.
		if isNotImplementedErr(err) {
			c.JSON(http.StatusNotImplemented, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_ = promptMessages

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("%s API error: status=%d, body=%s", prov.Name(), resp.StatusCode, string(body[:min(500, len(body))]))

		errorMsg := fmt.Sprintf("%s API error: %d", prov.Name(), resp.StatusCode)
		if len(body) > 0 {
			errorMsg = fmt.Sprintf("%s API error: %d - %s", prov.Name(), resp.StatusCode, string(body))
		}
		c.JSON(resp.StatusCode, gin.H{"error": errorMsg})
		return
	}

	// Request log埋点
	logStart := time.Now()
	defer func() {
		statusCode := c.Writer.Status()
		dur := time.Since(logStart)
		note := ""
		if req.Stream {
			note = "stream"
		} else {
			note = "non-stream"
		}
		LogRequest("POST", "/v1/chat/completions", model, statusCode, dur, note)
	}()

	if req.Stream {
		handleOpenAIStream(c, resp, model, req.Tools, prov.ParseStreamLine)
	} else {
		handleOpenAINonStream(c, resp, model, req.Tools, prov.ParseStreamLine)
	}
}

// isNotImplementedErr reports whether err is the qwen/kimi
// "not yet implemented" sentinel so the handler can return 501.
func isNotImplementedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "provider is not yet implemented")
}

// convertToProviderMessages converts internal/models.Message values
// to provider.Message values. The two types share the same shape so
// this is a straight field copy; kept as a helper so future divergence
// only touches one place.
func convertToProviderMessages(in []models.Message) []providers.Message {
	out := make([]providers.Message, len(in))
	for i, m := range in {
		out[i] = providers.Message{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCalls:  convertToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		}
	}
	return out
}

func convertToolCalls(in []models.ToolCall) []providers.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]providers.ToolCall, len(in))
	for i, tc := range in {
		out[i] = providers.ToolCall{
			ID:    tc.ID,
			Type:  tc.Type,
			Index: tc.Index,
			Function: providers.ToolFunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return out
}

// handleOpenAIStream handles streaming OpenAI response. The chunk
// parser is injected as a func so the same body works for any
// registered provider.
func handleOpenAIStream(c *gin.Context, resp *http.Response, model string, tools []models.Tool, parseStreamLine func(string) (*providers.StreamChunk, error)) {
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	var fullText strings.Builder
	var thinkingText strings.Builder
	var textBuffer string
	isFirstThinkChunk := true
	isFirstTextChunk := true
	inToolCall := false
	inNaturalToolCall := false
	hasTools := len(tools) > 0

	streamTimeout := time.NewTimer(120 * time.Second)
	defer streamTimeout.Stop()

	resetTimeout := func() {
		streamTimeout.Reset(120 * time.Second)
	}

	sendChunk := func(delta map[string]interface{}) {
		chunkID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli())
		chunk := map[string]interface{}{
			"id":      chunkID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         delta,
					"finish_reason": nil,
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		safeFlush(c.Writer)
	}

	flushTextBuffer := func() {
		if textBuffer != "" && !inToolCall && !inNaturalToolCall {
			delta := map[string]interface{}{"content": textBuffer}
			if isFirstTextChunk && isFirstThinkChunk {
				delta["role"] = "assistant"
			}
			isFirstTextChunk = false
			sendChunk(delta)
		}
		textBuffer = ""
	}

	sendTextChunk := func(text string) {
		if text == "" {
			return
		}
		delta := map[string]interface{}{"content": text}
		if isFirstTextChunk && isFirstThinkChunk {
			delta["role"] = "assistant"
		}
		isFirstTextChunk = false
		sendChunk(delta)
	}

	processLine := func(line string) {
		chunk, err := parseStreamLine(line)
		if err != nil || chunk == nil {
			return
		}

		if chunk.Type == "think" && chunk.Content != "" {
			thinkingText.WriteString(chunk.Content)
			delta := map[string]interface{}{"reasoning_content": chunk.Content}
			if isFirstThinkChunk {
				delta["role"] = "assistant"
				isFirstThinkChunk = false
			}
			sendChunk(delta)
		}

		if chunk.Type == "text" && chunk.Text != "" {
			fullText.WriteString(chunk.Text)
			textBuffer += chunk.Text

			if hasTools {
				startMatch := toolcall.DetectToolCallStartPublic(textBuffer, 0)
				if startMatch.Index != -1 && !inToolCall {
					if inNaturalToolCall {
						inNaturalToolCall = false
					}
					beforeTag := textBuffer[:startMatch.Index]
					textBuffer = textBuffer[startMatch.Index:]
					if beforeTag != "" {
						sendTextChunk(beforeTag)
					}
					inToolCall = true
					textBuffer = ""
				}

				if inToolCall {
					fullTextStr := fullText.String()
					endMatch := toolcall.DetectToolCallEndPublic(fullTextStr, 0)
					if endMatch.Index != -1 {
						inToolCall = false
						textBuffer = fullTextStr[endMatch.Index+len(endMatch.Tag):]
					}
				}

				if !inToolCall && !inNaturalToolCall {
					if strings.Contains(textBuffer, `"name"`) && strings.Contains(textBuffer, `"arguments"`) {
						natIdx := findNaturalToolStart(textBuffer)
						if natIdx != -1 {
							beforeNat := textBuffer[:natIdx]
							textBuffer = textBuffer[natIdx:]
							if beforeNat != "" {
								sendTextChunk(beforeNat)
							}
							inNaturalToolCall = true
						}
					}
				}

				if inNaturalToolCall {
					fullTextStr := fullText.String()
					fromNatStart := len(fullTextStr) - len(textBuffer)
					subText := fullTextStr[fromNatStart:]
					if balanced := toolcall.ExtractBalancedJSONPublic(subText, 0); balanced != "" {
						inNaturalToolCall = false
						textBuffer = fullTextStr[fromNatStart+len(balanced):]
					}
				}

				if !inToolCall && !inNaturalToolCall {
					tagLookback := toolcall.ToolCallStartLength()
					natLookback := toolcall.NaturalToolPrefixLookback(textBuffer)
					lookback := max(tagLookback, natLookback)
					safeLen := len(textBuffer) - lookback
					if safeLen > 0 {
						safeText := textBuffer[:safeLen]
						textBuffer = textBuffer[safeLen:]
						sendTextChunk(safeText)
					}
				}
			} else {
				flushTextBuffer()
			}
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// 自定义分割函数：确保不在UTF-8字符中间切断
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			end := i
			for end > 0 && !utf8.RuneStart(data[end]) {
				end--
			}
			if end < i && !utf8.Valid(data[end:i]) {
				return i + 1, data[0:end], nil
			}
			return i + 1, data[0:i], nil
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	done := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			resetTimeout()
			line := scanner.Text()
			processLine(line)
		}
		done <- true
	}()

	select {
	case <-done:
		if serr := scanner.Err(); serr != nil {
			log.Printf("Stream read error (OpenAI): status=%d err=%v", resp.StatusCode, serr)
			c.Writer.Header().Set("Content-Type", "text/event-stream")
			c.Writer.WriteHeader(http.StatusOK)
			errPayload, _ := json.Marshal(map[string]string{
				"error": fmt.Sprintf("upstream stream read failure (status=%d): %v", resp.StatusCode, serr),
			})
			fmt.Fprintf(c.Writer, "data: %s\n\n", string(errPayload))
			safeFlush(c.Writer)
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			safeFlush(c.Writer)
			resp.Body.Close()
			return
		}
	case <-streamTimeout.C:
		log.Printf("Stream timeout (OpenAI): no data for 120s, forcing end")
	}

	resp.Body.Close()

	fullTextStr := fullText.String()
	toolCalls := toolcall.ParseToolCalls(fullTextStr)
	hasToolCalls := len(toolCalls) > 0

	if hasToolCalls {
		cleanText := toolcall.StripToolCalls(fullTextStr)
		formattedCalls := toolcall.FormatToolCalls(toolCalls, 0)
		cleanText = strings.TrimSpace(cleanText)
		if cleanText != "" {
			delta := map[string]interface{}{"content": cleanText}
			if isFirstTextChunk && isFirstThinkChunk {
				delta["role"] = "assistant"
			}
			sendChunk(delta)
		}
		for i, fc := range formattedCalls {
			tc := fc
			tc["index"] = i
			delta := map[string]interface{}{
				"tool_calls": []map[string]interface{}{tc},
			}
			sendChunk(delta)
		}
	} else if textBuffer != "" && !inNaturalToolCall {
		delta := map[string]interface{}{"content": textBuffer}
		if isFirstTextChunk && isFirstThinkChunk {
			delta["role"] = "assistant"
		}
		sendChunk(delta)
	}

	finishReason := "stop"
	if hasToolCalls {
		finishReason = "tool_calls"
	}
	chunkID := fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli())
	finishChunk := map[string]interface{}{
		"id":      chunkID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": finishReason,
			},
		},
	}
	data, _ := json.Marshal(finishChunk)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	safeFlush(c.Writer)
}

// handleOpenAINonStream handles non-streaming OpenAI response. The
// chunk parser is injected as a func so the same body works for any
// registered provider.
func handleOpenAINonStream(c *gin.Context, resp *http.Response, model string, tools []models.Tool, parseStreamLine func(string) (*providers.StreamChunk, error)) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		log.Printf("upstream non-stream read failure: status=%d (partial body may be truncated) err=%v",
			resp.StatusCode, err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": fmt.Sprintf("upstream read failure (status=%d): %v", resp.StatusCode, err),
		})
		return
	}

	var fullText strings.Builder
	var thinkingText strings.Builder
	hasTools := len(tools) > 0

	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		chunk, err := parseStreamLine(line)
		if err != nil || chunk == nil {
			continue
		}
		if chunk.Type == "think" && chunk.Content != "" {
			thinkingText.WriteString(chunk.Content)
		}
		if chunk.Type == "text" && chunk.Text != "" {
			fullText.WriteString(chunk.Text)
		}
	}

	fullTextStr := fullText.String()
	toolCalls := []toolcall.ToolCall{}
	if hasTools {
		toolCalls = toolcall.ParseToolCalls(fullTextStr)
	}
	hasToolCalls := len(toolCalls) > 0
	cleanText := fullTextStr
	if hasToolCalls {
		cleanText = toolcall.StripToolCalls(fullTextStr)
	}

	content := fullTextStr
	if hasToolCalls {
		if cleanText == "" {
			content = ""
		} else {
			content = cleanText
		}
	}

	openaiMessage := models.ResponseMessage{
		Role: "assistant",
	}
	if hasToolCalls {
		openaiMessage.Content = nil
		if cleanText != "" {
			openaiMessage.Content = cleanText
		}
		formatted := toolcall.FormatToolCalls(toolCalls, 0)
		openaiToolCalls := make([]models.ToolCall, len(formatted))
		for i, fc := range formatted {
			fn := fc["function"].(map[string]interface{})
			openaiToolCalls[i] = models.ToolCall{
				ID:       fc["id"].(string),
				Type:     "function",
				Function: models.FunctionCall{
					Name:      fn["name"].(string),
					Arguments: fn["arguments"].(string),
				},
			}
		}
		openaiMessage.ToolCalls = openaiToolCalls
	} else {
		openaiMessage.Content = content
	}

	thinkingStr := thinkingText.String()
	if thinkingStr != "" {
		openaiMessage.ReasoningContent = thinkingStr
	}

	finishReason := "stop"
	if hasToolCalls {
		finishReason = "tool_calls"
	}

	response := models.OpenAIChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.Choice{
			{
				Index:        0,
				Message:      openaiMessage,
				FinishReason: finishReason,
			},
		},
		Usage: models.Usage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		},
	}

	c.JSON(http.StatusOK, response)
}

// findNaturalToolStart finds the start index of a natural tool call pattern in text
func findNaturalToolStart(text string) int {
	nameIdx := strings.Index(text, `"name"`)
	if nameIdx == -1 {
		nameIdx = strings.Index(text, "name")
		if nameIdx == -1 {
			return -1
		}
	}
	argsIdx := strings.Index(text[nameIdx:], `"arguments"`)
	if argsIdx == -1 {
		argsIdx = strings.Index(text[nameIdx:], "arguments")
		if argsIdx == -1 {
			return -1
		}
	}
	braceIdx := strings.LastIndex(text[:nameIdx], "{")
	if braceIdx == -1 {
		return -1
	}
	return braceIdx
}