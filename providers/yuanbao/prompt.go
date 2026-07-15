package yuanbao

import (
	"encoding/json"
	"strings"

	providers "yuanbao2api/providers"
)

// BuildPrompt renders a flat prompt string from cross-provider
// Message + Tool values. This implementation mirrors the existing
// OpenAI-style conversion in api/openai.go so the new provider
// package has the same prompt shape as before; anthropic-style
// conversion stays inside api/anthropic.go for now (its prompt is
// constructed inline before calling the registry).
//
// toolSystem is rendered separately so the OpenAI-style caller can
// append it to the prompt body if needed.
func BuildPrompt(messages []providers.Message, tools []providers.Tool) (string, string, error) {
	var b strings.Builder
	toolSystem := buildToolSystemPrompt(tools)
	systemInjected := false

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			content := stringifyContent(msg.Content)
			if toolSystem != "" && !strings.Contains(content, "[系统提示:") {
				b.WriteString("[系统提示: ")
				b.WriteString(content)
				b.WriteString(toolSystem)
				b.WriteString("]\n\n")
			} else {
				b.WriteString("[系统提示: ")
				b.WriteString(content)
				b.WriteString("]\n\n")
			}
			systemInjected = true
		case "user":
			b.WriteString("用户: ")
			b.WriteString(stringifyContent(msg.Content))
			b.WriteString("\n")
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var calls []string
				for _, tc := range msg.ToolCalls {
					calls = append(calls, "调用工具 "+tc.Function.Name+"，参数: "+tc.Function.Arguments)
				}
				b.WriteString("助手: 我需要调用工具来完成任务。\n")
				b.WriteString(strings.Join(calls, "\n"))
				b.WriteString("\n")
			} else {
				b.WriteString("助手: ")
				b.WriteString(stringifyContent(msg.Content))
				b.WriteString("\n")
			}
		case "tool":
			toolName := msg.Name
			if toolName == "" {
				toolName = "unknown"
			}
			b.WriteString("工具 ")
			b.WriteString(toolName)
			b.WriteString(" 的执行结果:\n")
			b.WriteString(stringifyContent(msg.Content))
			b.WriteString("\n\n")
		}
	}

	b.WriteString("\n请作为助手继续回复：")

	if toolSystem != "" && !systemInjected {
		b.WriteString("\n\n")
		b.WriteString(toolSystem)
	}
	return b.String(), toolSystem, nil
}

// BuildRequest assembles a *YuanbaoRequest from the prompt body and
// RequestOptions. This is a faithful port of the existing
// api/models.go buildYuanbaoRequest so that switching the handler to
// the registry does not change the upstream wire format.
func BuildRequest(prompt string, opts providers.RequestOptions) (*YuanbaoRequest, error) {
	chatModelID := opts.Model
	modelID := opts.Model
	subModelID := ""

	if opts.UseDeepThinking {
		if strings.Contains(opts.Model, "hunyuan") {
			chatModelID = "hunyuan_t1"
			modelID = "hunyuan_t1"
			subModelID = "hunyuan_t1"
		} else {
			chatModelID = "deep_seek"
			modelID = "deep_seek_v3"
			subModelID = "deep_seek"
		}
	}

	supportFunctions := []string{}
	if opts.UseInternet {
		supportFunctions = append(supportFunctions, "openInternetSearch")
	} else {
		supportFunctions = append(supportFunctions, "closeInternetSearch")
	}

	internetSearchFn := "closeInternetSearch"
	if opts.UseInternet {
		internetSearchFn = "openInternetSearch"
	}

	plugin := "Adaptive"
	if opts.UseDeepThinking {
		plugin = ""
	}

	displayPrompt := prompt
	if len(prompt) > 2000 {
		displayPrompt = prompt[:2000] + "...[已截断]"
	}

	extInfo, _ := json.Marshal(map[string]interface{}{
		"modelId":    modelID,
		"subModelId": subModelID,
		"supportFunctions": map[string]string{
			"internetSearch": internetSearchFn,
		},
	})

	return &YuanbaoRequest{
		Model:             modelID,
		Prompt:            prompt,
		Plugin:            plugin,
		DisplayPrompt:     displayPrompt,
		DisplayPromptType: 1,
		AgentID:           opts.AgentID,
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
	}, nil
}

// buildToolSystemPrompt renders the markdown-fragment that describes
// the available tools to the model.
func buildToolSystemPrompt(tools []providers.Tool) string {
	if len(tools) == 0 {
		return ""
	}
	var desc strings.Builder
	for _, tool := range tools {
		params := "{}"
		if tool.Function.Parameters != nil {
			switch v := tool.Function.Parameters.(type) {
			case string:
				params = v
			default:
				params = stringifyContent(v)
			}
		}
		desc.WriteString("### ")
		desc.WriteString(tool.Function.Name)
		desc.WriteString("\n")
		desc.WriteString(tool.Function.Description)
		desc.WriteString("\n参数:\n```json\n")
		desc.WriteString(params)
		desc.WriteString("```\n\n")
	}
	parts := []string{
		"",
		"# 可用工具",
		"你可以调用以下工具来完成任务。",
		"",
		"**重要：当你需要调用工具时，必须严格按照以下格式输出：**",
		"",
		"<|tool_calls_begin|>",
		`{"name": "函数名", "arguments": {"参数名": "参数值"}}`,
		"<|tool_calls_end|>",
		"",
		"注意事项：",
		"1. 工具调用必须用上述标记包裹，不要遗漏标记",
		"2. 标记内只能包含 JSON 格式的工具调用，不要添加其他文字",
		"3. 你可以同时调用多个工具，每个工具调用使用单独的标记对",
		"4. 如果不需要调用工具，直接回复用户即可，不要输出标记",
		"",
		"可用工具列表：",
		desc.String(),
		"",
	}
	return strings.Join(parts, "\n")
}

// stringifyContent is a tolerant renderer for Content values that may
// be a plain string, a slice of content blocks, or anything else
// (which gets formatted via JSON fallback).
func stringifyContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}