package notify

import (
	"strings"
	"time"
)

// ApplyTemplates 用配置模板重写 Title/Body。空模板保持解析器默认值；
// 未知占位符原样保留。用 strings.NewReplacer 单趟替换：替换结果不会被
// 二次扫描，因此正文里出现字面 "{event}" 不会链式展开。
func ApplyTemplates(msg Message, titleTemplate, bodyTemplate string) Message {
	if titleTemplate == "" && bodyTemplate == "" {
		return msg
	}
	replacer := strings.NewReplacer(templatePairs(msg)...)
	if titleTemplate != "" {
		msg.Title = replacer.Replace(titleTemplate)
	}
	if bodyTemplate != "" {
		msg.Body = replacer.Replace(bodyTemplate)
	}
	return msg
}

// templatePairs 展开占位符 → 渲染值。{message} 是解析器生成的默认正文，
// 必须在替换前取值，避免被 body 模板自身覆盖。
func templatePairs(msg Message) []string {
	return []string{
		"{agentName}", appDisplayName(msg.Agent),
		"{event}", eventDisplayName(msg.Event),
		"{project}", projectBase(msg.Workspace),
		"{workspace}", strings.TrimSpace(msg.Workspace),
		"{session}", sessionTag(msg.SessionID),
		"{message}", msg.Body,
		"{time}", time.Now().Format("15:04:05"),
	}
}

// sessionTag 截取会话 ID 前 8 个 rune 作为短标记；不足 8 位取全部。
func sessionTag(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	runes := []rune(sessionID)
	if len(runes) < 8 {
		return sessionID
	}
	return string(runes[:8])
}
