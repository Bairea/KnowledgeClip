package engine

import (
	"context"
	"encoding/json"
	"errors"
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
	return &RodEngine{browser: browser, db: db}
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

func (re *RodEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	var sels Selectors
	if err := json.Unmarshal([]byte(site.Selectors), &sels); err != nil {
		return "", err
	}
	if sels.Input == "" || sels.Submit == "" || sels.Answer == "" {
		return "", errors.New("missing required selectors")
	}

	page, err := re.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return "", err
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

	var answer string
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
				answer = text
				break
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	if answer == "" {
		return "", errors.New("poll answer timeout")
	}

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

	return answer, nil
}

func (re *RodEngine) Close() error {
	return re.browser.Close()
}
