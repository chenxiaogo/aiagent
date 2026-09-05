package service

// ContextBudget 将输入上下文按优先级限制在近似 token 预算内。
// 当前 OpenAI 兼容模型可能不暴露 tokenizer，因此统一采用 rune*1.3 的保守近似；
// 后续可替换为模型专用 tokenizer，不影响调用方。
type ContextBudget struct {
	MaxInputTokens        int
	ReservedOutputTokens  int
	MaxRuntimeMemoryRunes int
}

func DefaultContextBudget(maxTokens int) ContextBudget {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	return ContextBudget{
		MaxInputTokens:        maxTokens * 3,
		ReservedOutputTokens:  maxTokens,
		MaxRuntimeMemoryRunes: 4000,
	}
}

func estimateMessageTokens(message ChatMessage) int {
	return int(float64(len([]rune(message.Content))) * 1.3)
}

// TrimMessages 始终保留 system 和当前 user，从后向前保留最近历史。
func (b ContextBudget) TrimMessages(messages []ChatMessage) []ChatMessage {
	if len(messages) <= 2 || b.MaxInputTokens <= 0 {
		return messages
	}
	budget := b.MaxInputTokens
	selected := make([]bool, len(messages))
	selected[0] = true
	selected[len(messages)-1] = true
	budget -= estimateMessageTokens(messages[0]) + estimateMessageTokens(messages[len(messages)-1])
	for i := len(messages) - 2; i > 0 && budget > 0; i-- {
		cost := estimateMessageTokens(messages[i])
		if cost > budget {
			continue
		}
		selected[i] = true
		budget -= cost
	}
	out := make([]ChatMessage, 0, len(messages))
	for i, keep := range selected {
		if keep {
			out = append(out, messages[i])
		}
	}
	return out
}

func (b ContextBudget) TrimRuntimeMemory(content string) string {
	limit := b.MaxRuntimeMemoryRunes
	if limit <= 0 {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	return string(runes[:limit]) + "...<memory truncated>"
}
