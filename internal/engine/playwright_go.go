package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"chat-aggregator/internal/models"
	"github.com/playwright-community/playwright-go"
)

type PlaywrightGoEngine struct {
	pw      *playwright.Playwright
	browser playwright.Browser
}

func NewPlaywrightGoEngine() (*PlaywrightGoEngine, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, err
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		pw.Stop()
		return nil, err
	}

	return &PlaywrightGoEngine{pw: pw, browser: browser}, nil
}

func (pe *PlaywrightGoEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", err
	}
	if sels.Input == "" || sels.Submit == "" || sels.Answer == "" {
		return "", errors.New("missing required selectors")
	}

	page, err := pe.browser.NewPage()
	if err != nil {
		return "", err
	}
	defer page.Close()

	if _, err := page.Goto(site.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return "", err
	}

	if sels.WaitFor != "" {
		if _, err := page.WaitForSelector(sels.WaitFor, playwright.PageWaitForSelectorOptions{
			Timeout: playwright.Float(30000),
		}); err != nil {
			return "", err
		}
	}

	if err := page.Fill(sels.Input, prompt); err != nil {
		return "", err
	}

	if err := page.Click(sels.Submit); err != nil {
		return "", err
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		el, err := page.QuerySelector(sels.Answer)
		if err == nil && el != nil {
			text, err := el.TextContent()
			if err == nil && text != "" {
				return text, nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return "", errors.New("poll answer timeout")
}

func (pe *PlaywrightGoEngine) Close() error {
	if pe.browser != nil {
		pe.browser.Close()
	}
	if pe.pw != nil {
		pe.pw.Stop()
	}
	return nil
}
