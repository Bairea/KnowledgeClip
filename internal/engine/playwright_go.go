package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"chat-aggregator/internal/models"
	"chat-aggregator/internal/storage"
	"github.com/playwright-community/playwright-go"
)

type PlaywrightGoEngine struct {
	pw      *playwright.Playwright
	browser playwright.Browser
	db      *storage.DB
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

func (pe *PlaywrightGoEngine) SetDB(db *storage.DB) {
	pe.db = db
}

func cookiesToOptional(in []playwright.Cookie) []playwright.OptionalCookie {
	out := make([]playwright.OptionalCookie, 0, len(in))
	for _, c := range in {
		oc := playwright.OptionalCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   &c.Domain,
			Path:     &c.Path,
			Expires:  &c.Expires,
			HttpOnly: &c.HttpOnly,
			Secure:   &c.Secure,
			SameSite: c.SameSite,
		}
		out = append(out, oc)
	}
	return out
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

	if pe.db != nil {
		if siteCookie, err := storage.GetSiteCookie(pe.db, site.ID); err == nil && siteCookie != nil && siteCookie.Cookies != "" {
			var stored []playwright.Cookie
			if json.Unmarshal([]byte(siteCookie.Cookies), &stored) == nil && len(stored) > 0 {
				_ = page.Context().AddCookies(cookiesToOptional(stored))
			}
		}
	}

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
	var lastText string
	stableRounds := 0
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
				if text == lastText {
					stableRounds++
					if stableRounds >= 3 {
						if pe.db != nil {
							if rawCookies, cerr := page.Context().Cookies(); cerr == nil && len(rawCookies) > 0 {
								if data, mErr := json.Marshal(rawCookies); mErr == nil {
									_ = storage.SaveSiteCookie(pe.db, models.SiteCookie{
										SiteID:  site.ID,
										Cookies: string(data),
									})
								}
							}
						}
						return text, nil
					}
				} else {
					stableRounds = 0
					lastText = text
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	if lastText != "" && pe.db != nil {
		if rawCookies, cerr := page.Context().Cookies(); cerr == nil && len(rawCookies) > 0 {
			if data, mErr := json.Marshal(rawCookies); mErr == nil {
				_ = storage.SaveSiteCookie(pe.db, models.SiteCookie{
					SiteID:  site.ID,
					Cookies: string(data),
				})
			}
		}
	}

	return lastText, nil
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

func (pe *PlaywrightGoEngine) Name() string {
	return "playwright-go"
}
