package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"chat-aggregator/internal/models"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type RodEngine struct {
	browser *rod.Browser
}

type Selectors struct {
	Input   string `json:"input"`
	Submit  string `json:"submit"`
	Answer  string `json:"answer"`
	WaitFor string `json:"wait_for"`
}

func NewRodEngine() *RodEngine {
	u := launcher.New().Headless(false).Devtools(true).MustLaunch()
	browser := rod.New().ControlURL(u).MustConnect()
	return &RodEngine{browser: browser}
}

func (re *RodEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", err
	}
	if sels.Input == "" || sels.Submit == "" || sels.Answer == "" {
		return "", errors.New("missing required selectors")
	}

	page, err := re.browser.Page(proto.TargetCreateTarget{URL: site.URL})
	if err != nil {
		return "", err
	}

	if sels.WaitFor != "" {
		if _, err := page.Timeout(30 * time.Second).Element(sels.WaitFor); err != nil {
			return "", err
		}
	}

	inputEl, err := page.Element(sels.Input)
	if err != nil {
		return "", err
	}
	if err := inputEl.Input(prompt); err != nil {
		return "", err
	}

	submitEl, err := page.Element(sels.Submit)
	if err != nil {
		return "", err
	}
	if err := submitEl.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return "", err
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		els, err := page.Elements(sels.Answer)
		if err == nil && len(els) > 0 {
			text, err := els[0].Text()
			if err == nil && text != "" {
				return text, nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return "", errors.New("poll answer timeout")
}

func (re *RodEngine) Close() error {
	return re.browser.Close()
}
