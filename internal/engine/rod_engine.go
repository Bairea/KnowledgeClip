package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type RodEngine struct {
	browser *rod.Browser
	db      *storage.DB
	pages   map[string]*rod.Page
	mu      sync.Mutex
}

type Selectors struct {
	Input   string `json:"input"`
	Submit  string `json:"submit"`
	Answer  string `json:"answer"`
	WaitFor string `json:"wait_for"`
}

func NewRodEngine(db *storage.DB) *RodEngine {
	u := launcher.New().Headless(false).Devtools(true).MustLaunch()
	browser := rod.New().ControlURL(u).MustConnect()
	return &RodEngine{
		browser: browser,
		db:      db,
		pages:   make(map[string]*rod.Page),
	}
}

func toNetworkCookieParam(c *proto.NetworkCookie) *proto.NetworkCookieParam {
	p := &proto.NetworkCookieParam{
		Name:         c.Name,
		Value:        c.Value,
		Domain:       c.Domain,
		Path:         c.Path,
		Expires:      c.Expires,
		Secure:       c.Secure,
		HTTPOnly:     c.HTTPOnly,
		SameSite:     c.SameSite,
		Priority:     c.Priority,
		SameParty:    c.SameParty,
		SourceScheme: c.SourceScheme,
		PartitionKey: c.PartitionKey,
	}
	if c.SourcePort != 0 {
		p.SourcePort = &c.SourcePort
	}
	return p
}

func (re *RodEngine) getOrCreatePage(site models.Site) (*rod.Page, error) {
	re.mu.Lock()
	defer re.mu.Unlock()

	if page, ok := re.pages[site.ID]; ok && page != nil {
		return page, nil
	}

	page, err := re.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}

	if re.db != nil {
		siteCookie, err := storage.GetSiteCookie(re.db, site.ID)
		if err == nil && siteCookie != nil && siteCookie.Cookies != "" {
			var cookies []*proto.NetworkCookieParam
			if json.Unmarshal([]byte(siteCookie.Cookies), &cookies) == nil && len(cookies) > 0 {
				_ = page.SetCookies(cookies)
			}
		}
	}

	if err := page.Navigate(site.URL); err != nil {
		page.Close()
		return nil, fmt.Errorf("navigate: %w", err)
	}

	if err := page.WaitLoad(); err != nil {
		page.Close()
		return nil, fmt.Errorf("wait load: %w", err)
	}

	if err := page.WaitIdle(3 * time.Second); err != nil {
		// non-fatal, some pages never truly idle
	}

	re.pages[site.ID] = page
	return page, nil
}

func (re *RodEngine) ResetPages() {
	re.mu.Lock()
	defer re.mu.Unlock()

	for id, page := range re.pages {
		if page != nil {
			_ = page.Close()
		}
		delete(re.pages, id)
	}
}

func (re *RodEngine) typePrompt(page *rod.Page, selector string, prompt string) error {
	el, err := page.Timeout(10 * time.Second).Element(selector)
	if err != nil {
		return fmt.Errorf("find input element %s: %w", selector, err)
	}

	// Try native Input first (works for <input> and <textarea>)
	if err := el.Input(prompt); err == nil {
		return nil
	}

	// Fallback: use JavaScript for contenteditable and custom editors
	js := fmt.Sprintf(`
		(function() {
			var el = document.querySelector(%q);
			if (!el) return false;
			if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
				el.value = %q;
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
			} else if (el.isContentEditable) {
				el.innerText = %q;
				el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q }));
			} else {
				el.textContent = %q;
				el.dispatchEvent(new Event('input', { bubbles: true }));
			}
			return true;
		})()
	`, selector, prompt, prompt, prompt, prompt)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("js input failed: %w", err)
	}
	if result.Value.Bool() == false {
		return errors.New("js input returned false: element not found")
	}
	return nil
}

func (re *RodEngine) submitPrompt(page *rod.Page, selector string) error {
	// Try clicking the submit button
	el, err := page.Timeout(10 * time.Second).Element(selector)
	if err == nil {
		if clickErr := el.Click(proto.InputMouseButtonLeft, 1); clickErr == nil {
			return nil
		}
	}

	// Fallback: trigger Enter key on the input element (some sites use Enter to send)
	js := fmt.Sprintf(`
		(function() {
			var el = document.querySelector(%q);
			if (!el) return false;
			var ev = new KeyboardEvent('keydown', {
				key: 'Enter',
				code: 'Enter',
				keyCode: 13,
				which: 13,
				bubbles: true,
				cancelable: true
			});
			el.dispatchEvent(ev);
			return true;
		})()
	`, selector)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("js submit failed: %w", err)
	}
	if result.Value.Bool() == false {
		return errors.New("js submit returned false: element not found")
	}
	return nil
}

func (re *RodEngine) countAnswers(page *rod.Page, selector string) int {
	els, err := page.Elements(selector)
	if err != nil {
		return 0
	}
	return len(els)
}

func (re *RodEngine) getLastAnswerText(page *rod.Page, selector string) (string, error) {
	els, err := page.Elements(selector)
	if err != nil {
		return "", err
	}
	if len(els) == 0 {
		return "", errors.New("no answer elements")
	}
	last := els[len(els)-1]
	return last.Text()
}

func (re *RodEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", fmt.Errorf("parse selectors: %w", err)
	}
	if sels.Input == "" || sels.Answer == "" {
		return "", errors.New("missing required selectors: input and answer are required")
	}

	page, err := re.getOrCreatePage(site)
	if err != nil {
		return "", fmt.Errorf("get page: %w", err)
	}

	// Wait for input element to be ready
	if sels.WaitFor != "" {
		if _, err := page.Timeout(30 * time.Second).Element(sels.WaitFor); err != nil {
			return "", fmt.Errorf("wait_for timeout (%s): %w", sels.WaitFor, err)
		}
	}

	// Record answer count before sending
	beforeCount := re.countAnswers(page, sels.Answer)

	// Type prompt
	if err := re.typePrompt(page, sels.Input, prompt); err != nil {
		return "", fmt.Errorf("type prompt: %w", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Submit
	if sels.Submit != "" {
		if err := re.submitPrompt(page, sels.Submit); err != nil {
			return "", fmt.Errorf("submit: %w", err)
		}
	} else {
		// No submit selector: try Enter on input
		if err := re.submitPrompt(page, sels.Input); err != nil {
			return "", fmt.Errorf("submit (Enter on input): %w", err)
		}
	}

	// Poll for new answer: either count increased or text changed
	var lastText string
	stableRounds := 0
	const requiredStable = 3
	deadline := time.Now().Add(120 * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		currentCount := re.countAnswers(page, sels.Answer)
		if currentCount > beforeCount {
			// New answer appeared, get its text
			text, err := re.getLastAnswerText(page, sels.Answer)
			if err == nil && text != "" {
				if text == lastText {
					stableRounds++
					if stableRounds >= requiredStable {
						lastText = text
						goto done
					}
				} else {
					stableRounds = 0
					lastText = text
				}
			}
		} else if currentCount == beforeCount && beforeCount > 0 {
			// Same count but text may have changed (streaming update)
			text, err := re.getLastAnswerText(page, sels.Answer)
			if err == nil && text != "" {
				if text == lastText {
					stableRounds++
					if stableRounds >= requiredStable {
						lastText = text
						goto done
					}
				} else {
					stableRounds = 0
					lastText = text
				}
			}
		} else if currentCount == 0 {
			// No answers yet, keep waiting
		}

		time.Sleep(500 * time.Millisecond)
	}

	if lastText == "" {
		return "", errors.New("poll answer timeout: no answer received within 120s")
	}

done:
	if re.db != nil {
		cookies, err := page.Cookies(nil)
		if err == nil && len(cookies) > 0 {
			params := make([]*proto.NetworkCookieParam, len(cookies))
			for i, c := range cookies {
				params[i] = toNetworkCookieParam(c)
			}
			cookieData, marshalErr := json.Marshal(params)
			if marshalErr == nil {
				_ = storage.SaveSiteCookie(re.db, models.SiteCookie{
					SiteID:  site.ID,
					Cookies: string(cookieData),
				})
			}
		}
	}

	return lastText, nil
}

func (re *RodEngine) Close() error {
	re.ResetPages()
	return re.browser.Close()
}

func (re *RodEngine) Name() string {
	return "rod"
}
