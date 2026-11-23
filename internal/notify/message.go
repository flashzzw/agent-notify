package notify

import "context"

type Message struct {
	Agent     string
	Event     string
	SessionID string
	Workspace string
	Title     string
	Body      string
	// 以下是未经排版的原始字段，供「转发给 magpie 插件」使用：文案由插件生成，
	// 所以这里保留工具名、原始文本与模型名，而不是只有排好版的 Title/Body。
	Tool   string
	Detail string
	Model  string
}
type Sender interface {
	Name() string
	Send(context.Context, Message) error
}
