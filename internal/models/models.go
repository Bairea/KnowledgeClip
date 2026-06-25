package models

import "time"

type Site struct {
	ID           int
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
	ID        int
	Prompt    string
	CreatedAt time.Time
}

type Message struct {
	ID        int
	SessionID int
	SiteID    int
	Content   string
	Kept      bool
	Error     string
	ElapsedMs int
	CreatedAt time.Time
}

type SiteCookie struct {
	SiteID       int
	Cookies      string
	LocalStorage string
	UpdatedAt    time.Time
}
