package models

import "time"

type Site struct {
	ID           string
	Name         string
	URL          string
	EngineType   string
	Selectors    string
	CookieFile   string
	Enabled      bool
	FormatPrompt string
	CreatedAt    time.Time
}

type Session struct {
	ID        string
	Prompt    string
	CreatedAt time.Time
}

type Message struct {
	ID        string
	SessionID string
	SiteID    string
	Content   string
	Kept      bool
	Error     string
	ElapsedMs int
	CreatedAt time.Time
}

type SiteCookie struct {
	SiteID       string
	Cookies      string
	LocalStorage string
	UpdatedAt    time.Time
}
