package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"chat-aggregator/internal/models"
)

// TestBskLiveSixSites drives all six domestic chat sites end-to-end through
// the bsk engine (send → wait → extract), reusing the per-site
// scripts/browser-act/<site>/*.js scripts.
//
// Gated by BSK_LIVE_TEST=1: it sends a real structured prompt to the user's
// logged-in accounts, so it must not run in CI or unattended test runs.
// Requires the browser-skill extension to be connected (`bsk status`).
func TestBskLiveSixSites(t *testing.T) {
	if os.Getenv("BSK_LIVE_TEST") != "1" {
		t.Skip("set BSK_LIVE_TEST=1 to run the live bsk test against real sites")
	}

	// Deterministic script location: repo-relative from internal/engine.
	_ = os.Setenv("BROWSER_ACT_SCRIPTS_DIR", "../../scripts/browser-act")

	sites := []models.Site{
		{ID: "qwen", Name: "Qwen", URL: "https://www.qianwen.com/"},
		{ID: "kimi", Name: "Kimi", URL: "https://www.kimi.com/"},
		{ID: "deepseek", Name: "DeepSeek", URL: "https://chat.deepseek.com/"},
		{ID: "minimax", Name: "MiniMax", URL: "https://agent.minimaxi.com/"},
		{ID: "glm", Name: "GLM", URL: "https://chatglm.cn/"},
		{ID: "doubao", Name: "Doubao", URL: "https://www.doubao.com/chat/"},
	}

	eng, err := NewBskEngine(getScriptsDir())
	if err != nil {
		t.Fatalf("new bsk engine: %v", err)
	}
	defer func() {
		if err := eng.Close(); err != nil {
			t.Logf("engine close: %v", err)
		}
	}()

	// Structured prompt so extraction fidelity (headings / code / table) is
	// exercised on every site.
	prompt := "请用 Markdown 格式回答：\n1. 用一句话自我介绍\n2. 给出一个 Python 打印 hello world 的代码块\n3. 给出一个 2 行 2 列的表格"

	for _, site := range sites {
		t.Run(site.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()

			start := time.Now()
			answer, err := eng.SendMessage(ctx, site, prompt)
			elapsed := time.Since(start)
			if err != nil {
				t.Errorf("site %s FAILED after %s: %v", site.ID, elapsed, err)
				return
			}
			t.Logf("site %s OK in %s, %d chars", site.ID, elapsed, len(answer))
			t.Logf("site %s answer:\n%s", site.ID, answer)
			if len(answer) < 50 {
				t.Errorf("site %s answer suspiciously short (%d chars)", site.ID, len(answer))
			}
		})
	}
}