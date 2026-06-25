package engine

import (
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

var selectorCandidates = map[string][]string{
	"input": {
		"#chat-input",
		"textarea",
		"[contenteditable=true]",
		"input[type=text]",
	},
	"submit": {
		"button[type=submit]",
		"#send-btn",
		".send-button",
	},
	"answer": {
		".message-content",
		".answer-content",
		"[class*=message]",
		"[class*=answer]",
	},
}

func DetectSelectors(url string) (map[string]string, error) {
	l := launcher.New().Headless(true).NoSandbox(true)
	u, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(u).MustConnect()
	defer browser.Close()

	page := browser.MustPage("")
	defer page.Close()

	if err := page.Navigate(url); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("wait load: %w", err)
	}

	// Give dynamic content a moment to render
	time.Sleep(2 * time.Second)

	result := make(map[string]string)

	for key, candidates := range selectorCandidates {
		for _, sel := range candidates {
			found, _, err := page.Has(sel)
			if err == nil && found {
				result[key] = sel
				break
			}
		}
	}

	if answerSel, ok := result["answer"]; ok {
		result["wait_for"] = answerSel + ":last-child"
	}

	return result, nil
}
