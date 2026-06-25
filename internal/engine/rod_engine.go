package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
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

func findChromeBinary() string {
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		`C:\Users\baizhicong\AppData\Local\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func NewRodEngine(db *storage.DB) *RodEngine {
	l := launcher.New()

	chromePath := findChromeBinary()
	if chromePath != "" {
		log.Printf("[rod] using browser binary: %s", chromePath)
		l = l.Bin(chromePath)
	} else {
		log.Printf("[rod] no system Chrome found, rod will try to download")
	}

	userDataDir := "./.browser-data"
	l = l.UserDataDir(userDataDir)
	l = l.Headless(false).Devtools(true)

	u, err := l.Launch()
	if err != nil {
		log.Printf("[rod] launch failed: %v", err)
		return &RodEngine{db: db, pages: make(map[string]*rod.Page)}
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		log.Printf("[rod] connect failed: %v", err)
		return &RodEngine{db: db, pages: make(map[string]*rod.Page)}
	}

	log.Printf("[rod] browser connected successfully")
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
		log.Printf("[rod] reusing existing page for site %s", site.ID)
		return page, nil
	}

	log.Printf("[rod] creating new page for site %s -> %s", site.ID, site.URL)
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
				log.Printf("[rod] injected %d cookies for site %s", len(cookies), site.ID)
			}
		}
	}

	if err := page.Navigate(site.URL); err != nil {
		page.Close()
		return nil, fmt.Errorf("navigate to %s: %w", site.URL, err)
	}

	log.Printf("[rod] waiting for page load: %s", site.URL)
	if err := page.WaitLoad(); err != nil {
		log.Printf("[rod] WaitLoad failed (non-fatal): %v", err)
	}

	_ = page.WaitIdle(3 * time.Second)

	re.pages[site.ID] = page
	log.Printf("[rod] page ready for site %s", site.ID)
	return page, nil
}

func (re *RodEngine) typePrompt(page *rod.Page, selector string, prompt string) error {
	log.Printf("[rod] looking for input element: %s", selector)
	el, err := page.Timeout(15 * time.Second).Element(selector)
	if err != nil {
		return fmt.Errorf("find input element %s: %w", selector, err)
	}
	log.Printf("[rod] found input element, typing prompt (%d chars)", len(prompt))

	if err := el.Input(prompt); err == nil {
		log.Printf("[rod] Input() succeeded")
		return nil
	}
	log.Printf("[rod] Input() failed, trying JS fallback")

	js := fmt.Sprintf(`
		(function() {
			var el = document.querySelector(%q);
			if (!el) return false;
			if (el.tagName === 'TEXTAREA' || el.tagName === 'INPUT') {
				var nativeSetter = Object.getOwnPropertyDescriptor(
					window.HTMLTextAreaElement.prototype, 'value'
				) || Object.getOwnPropertyDescriptor(
					window.HTMLInputElement.prototype, 'value'
				);
				if (nativeSetter && nativeSetter.set) {
					nativeSetter.set.call(el, %q);
				} else {
					el.value = %q;
				}
				el.dispatchEvent(new Event('input', { bubbles: true }));
				el.dispatchEvent(new Event('change', { bubbles: true }));
			} else if (el.isContentEditable) {
				el.focus();
				el.innerText = %q;
				el.dispatchEvent(new InputEvent('input', { bubbles: true, data: %q }));
			} else {
				el.textContent = %q;
				el.dispatchEvent(new Event('input', { bubbles: true }));
			}
			return true;
		})()
	`, selector, prompt, prompt, prompt, prompt, prompt)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("js input failed: %w", err)
	}
	if !result.Value.Bool() {
		return errors.New("js input returned false: element not found")
	}
	log.Printf("[rod] JS input succeeded")
	return nil
}

func (re *RodEngine) submitPrompt(page *rod.Page, selector string) error {
	log.Printf("[rod] looking for submit element: %s", selector)
	el, err := page.Timeout(10 * time.Second).Element(selector)
	if err == nil {
		log.Printf("[rod] found submit element, clicking")
		clickErr := el.Click(proto.InputMouseButtonLeft, 1)
		if clickErr == nil {
			log.Printf("[rod] click succeeded")
			return nil
		}
		log.Printf("[rod] click failed: %v, trying JS", clickErr)
	} else {
		log.Printf("[rod] submit element not found: %v, trying Enter key", err)
	}

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
			el.dispatchEvent(new KeyboardEvent('keyup', {
				key: 'Enter',
				code: 'Enter',
				keyCode: 13,
				which: 13,
				bubbles: true
			}));
			return true;
		})()
	`, selector)

	result, err := page.Eval(js)
	if err != nil {
		return fmt.Errorf("js submit failed: %w", err)
	}
	if !result.Value.Bool() {
		return errors.New("js submit returned false: element not found")
	}
	log.Printf("[rod] JS submit (Enter key) succeeded")
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
	log.Printf("[rod] SendMessage start: site=%s prompt=%q", site.ID, prompt[:min(50, len(prompt))])

	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", fmt.Errorf("parse selectors: %w", err)
	}
	log.Printf("[rod] selectors: input=%s submit=%s answer=%s wait_for=%s",
		sels.Input, sels.Submit, sels.Answer, sels.WaitFor)

	if sels.Input == "" || sels.Answer == "" {
		return "", errors.New("missing required selectors: input and answer are required")
	}

	page, err := re.getOrCreatePage(site)
	if err != nil {
		return "", fmt.Errorf("get page: %w", err)
	}

	if sels.WaitFor != "" {
		log.Printf("[rod] waiting for element: %s", sels.WaitFor)
		if _, err := page.Timeout(30 * time.Second).Element(sels.WaitFor); err != nil {
			log.Printf("[rod] wait_for timeout (non-fatal): %v", err)
		}
	}

	beforeCount := re.countAnswers(page, sels.Answer)
	log.Printf("[rod] answer count before sending: %d", beforeCount)

	if err := re.typePrompt(page, sels.Input, prompt); err != nil {
		return "", fmt.Errorf("type prompt: %w", err)
	}
	time.Sleep(500 * time.Millisecond)

	if sels.Submit != "" {
		if err := re.submitPrompt(page, sels.Submit); err != nil {
			log.Printf("[rod] submit failed, trying Enter on input: %v", err)
			_ = re.submitPrompt(page, sels.Input)
		}
	} else {
		_ = re.submitPrompt(page, sels.Input)
	}
	time.Sleep(1 * time.Second)

	var lastText string
	stableRounds := 0
	const requiredStable = 3
	deadline := time.Now().Add(120 * time.Second)
	pollCount := 0

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		pollCount++

		currentCount := re.countAnswers(page, sels.Answer)
		if currentCount > beforeCount {
			text, err := re.getLastAnswerText(page, sels.Answer)
			if err == nil && text != "" {
				if text == lastText {
					stableRounds++
					if stableRounds >= requiredStable {
						log.Printf("[rod] answer stabilized after %d polls (%d chars)", pollCount, len(text))
						lastText = text
						goto done
					}
				} else {
					stableRounds = 0
					lastText = text
					if pollCount <= 5 || pollCount%20 == 0 {
						log.Printf("[rod] polling... count=%d text_len=%d", currentCount, len(text))
					}
				}
			}
		} else if currentCount == beforeCount && beforeCount > 0 {
			text, err := re.getLastAnswerText(page, sels.Answer)
			if err == nil && text != "" && text != lastText {
				stableRounds = 0
				lastText = text
			} else if err == nil && text != "" && text == lastText {
				stableRounds++
				if stableRounds >= requiredStable {
					log.Printf("[rod] answer stabilized (same count) after %d polls", pollCount)
					goto done
				}
			}
		}

		if pollCount%20 == 0 {
			log.Printf("[rod] still polling... count=%d before=%d poll=%d", currentCount, beforeCount, pollCount)
		}

		time.Sleep(500 * time.Millisecond)
	}

	if lastText == "" {
		log.Printf("[rod] poll timeout: no answer received within 120s (polls=%d)", pollCount)
		return "", errors.New("poll answer timeout: no answer received within 120s")
	}

done:
	log.Printf("[rod] answer received: %d chars", len(lastText))

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

func (re *RodEngine) ResetPages() {
	re.mu.Lock()
	defer re.mu.Unlock()

	for id, page := range re.pages {
		if page != nil {
			_ = page.Close()
		}
		delete(re.pages, id)
	}
	log.Printf("[rod] all pages reset")
}

func (re *RodEngine) Close() error {
	re.ResetPages()
	if re.browser != nil {
		return re.browser.Close()
	}
	return nil
}

func (re *RodEngine) Name() string {
	return "rod"
}
