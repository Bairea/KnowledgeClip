package engine

import "context"

// Progress reporting flows from engines to the API layer through the
// request context: chat.go installs a per-site ProgressFunc via
// WithProgress, engines call ReportProgress at phase boundaries, and the
// callback broadcasts a WebSocket progress event. Engines that skip
// reporting simply see a no-op.

type progressKeyType struct{}

var progressKey progressKeyType

// Coarse pipeline stages (safe to persist in UI state machines).
const (
	ProgressInput      = "input"      // 页面/输入框准备中
	ProgressSending    = "sending"    // 发送提问中
	ProgressGenerating = "generating" // 模型生成中
	ProgressExtracting = "extracting" // 提取回答中
)

// ProgressFunc receives each stage transition. Called from the engine's
// send goroutine; implementations must be cheap and non-blocking.
type ProgressFunc func(stage string)

// WithProgress returns a context carrying fn.
func WithProgress(ctx context.Context, fn ProgressFunc) context.Context {
	return context.WithValue(ctx, progressKey, fn)
}

// ReportProgress emits a stage transition when the context carries one.
func ReportProgress(ctx context.Context, stage string) {
	if fn, ok := ctx.Value(progressKey).(ProgressFunc); ok && fn != nil {
		fn(stage)
	}
}
