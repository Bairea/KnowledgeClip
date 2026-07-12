package models

import "time"

type Site struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	EngineType   string    `json:"engine_type"`
	Selectors    string    `json:"selectors"`
	CookieFile   string    `json:"cookie_file"`
	Enabled      bool      `json:"enabled"`
	Selected     bool      `json:"selected"`
	FormatPrompt string    `json:"format_prompt"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	CreatedAt time.Time `json:"created_at"`
}

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	SiteID    string    `json:"site_id"`
	Content   string    `json:"content"`
	Kept      bool      `json:"kept"`
	Error     string    `json:"error"`
	ElapsedMs int       `json:"elapsed_ms"`
	Turn      int       `json:"turn"`
	Prompt    string    `json:"prompt"`
	CreatedAt time.Time `json:"created_at"`
}

type SiteCookie struct {
	SiteID       string    `json:"site_id"`
	Cookies      string    `json:"cookies"`
	LocalStorage string    `json:"local_storage"`
	UpdatedAt    time.Time `json:"updated_at"`
}
