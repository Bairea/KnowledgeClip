package storage

import (
	"database/sql"
	"fmt"

	"chat-aggregator/internal/models"
)

func GetSiteCookie(db *DB, siteID string) (*models.SiteCookie, error) {
	row := db.Conn().QueryRow(
		`SELECT site_id, cookies, local_storage, updated_at FROM site_cookies WHERE site_id = ?`,
		siteID,
	)
	var c models.SiteCookie
	if err := row.Scan(&c.SiteID, &c.Cookies, &c.LocalStorage, &c.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan site cookie: %w", err)
	}
	return &c, nil
}

func SaveSiteCookie(db *DB, cookie models.SiteCookie) error {
	_, err := db.Conn().Exec(
		`INSERT OR REPLACE INTO site_cookies (site_id, cookies, local_storage, updated_at) VALUES (?, ?, ?, datetime('now'))`,
		cookie.SiteID, cookie.Cookies, cookie.LocalStorage,
	)
	if err != nil {
		return fmt.Errorf("save site cookie: %w", err)
	}
	return nil
}
