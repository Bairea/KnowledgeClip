package storage

import (
	"chat-aggregator/internal/models"
	"database/sql"
	"fmt"
)

func SyncSites(db *DB, sites []models.Site) error {
	conn := db.Conn()
	tx, err := conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO sites (id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			url = excluded.url,
			engine_type = excluded.engine_type,
			selectors = excluded.selectors,
			cookie_file = excluded.cookie_file,
			enabled = excluded.enabled,
			format_prompt = excluded.format_prompt
	`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, site := range sites {
		enabled := 0
		if site.Enabled {
			enabled = 1
		}
		_, err := stmt.Exec(site.ID, site.Name, site.URL, site.EngineType, site.Selectors, site.CookieFile, enabled, site.FormatPrompt)
		if err != nil {
			return fmt.Errorf("upsert site %s: %w", site.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func GetSites(db *DB) ([]models.Site, error) {
	rows, err := db.Conn().Query(`
		SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, created_at
		FROM sites
	`)
	if err != nil {
		return nil, fmt.Errorf("query sites: %w", err)
	}
	defer rows.Close()

	var sites []models.Site
	for rows.Next() {
		var site models.Site
		var enabled int
		if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &site.FormatPrompt, &site.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan site: %w", err)
		}
		site.Enabled = enabled != 0
		sites = append(sites, site)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sites: %w", err)
	}

	return sites, nil
}

func GetSiteByID(db *DB, id string) (*models.Site, error) {
	var site models.Site
	var enabled int
	row := db.Conn().QueryRow(`
		SELECT id, name, url, engine_type, selectors, cookie_file, enabled, format_prompt, created_at
		FROM sites
		WHERE id = ?
	`, id)
	if err := row.Scan(&site.ID, &site.Name, &site.URL, &site.EngineType, &site.Selectors, &site.CookieFile, &enabled, &site.FormatPrompt, &site.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan site: %w", err)
	}
	site.Enabled = enabled != 0
	return &site, nil
}
