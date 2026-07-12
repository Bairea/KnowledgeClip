package storage

import (
	"testing"
	"time"

	"chat-aggregator/internal/models"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGetSession(t *testing.T) {
	db := newTestDB(t)

	err := CreateSession(db, "sess-1", "What is Go?")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	session, err := GetSessionByID(db, "sess-1")
	if err != nil {
		t.Fatalf("GetSessionByID failed: %v", err)
	}
	if session == nil {
		t.Fatal("expected session to be non-nil")
	}
	if session.ID != "sess-1" {
		t.Errorf("expected session ID 'sess-1', got '%s'", session.ID)
	}
	if session.Prompt != "What is Go?" {
		t.Errorf("expected prompt 'What is Go?', got '%s'", session.Prompt)
	}
}

func TestGetSessionByID_NotFound(t *testing.T) {
	db := newTestDB(t)

	session, err := GetSessionByID(db, "nonexistent")
	if err != nil {
		t.Fatalf("GetSessionByID returned error: %v", err)
	}
	if session != nil {
		t.Error("expected nil session for nonexistent ID")
	}
}

func TestGetSessions_Empty(t *testing.T) {
	db := newTestDB(t)

	sessions, err := GetSessions(db)
	if err != nil {
		t.Fatalf("GetSessions failed: %v", err)
	}
	if sessions == nil {
		t.Fatal("expected non-nil sessions slice")
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestCreateAndGetMessages(t *testing.T) {
	db := newTestDB(t)

	if err := CreateSession(db, "sess-1", "Test prompt"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	msg1 := models.Message{
		ID: "msg-1", SessionID: "sess-1", SiteID: "kimi",
		Content: "Answer from Kimi", ElapsedMs: 5000, Turn: 1, Prompt: "Test prompt",
	}
	if err := CreateMessage(db, msg1.ID, msg1.SessionID, msg1.SiteID, msg1.Content, "", msg1.ElapsedMs, msg1.Turn, msg1.Prompt); err != nil {
		t.Fatalf("CreateMessage msg-1 failed: %v", err)
	}

	msg2 := models.Message{
		ID: "msg-2", SessionID: "sess-1", SiteID: "deepseek",
		Content: "Answer from DeepSeek", ElapsedMs: 3000, Turn: 1, Prompt: "Test prompt",
	}
	if err := CreateMessage(db, msg2.ID, msg2.SessionID, msg2.SiteID, msg2.Content, "timeout", msg2.ElapsedMs, msg2.Turn, msg2.Prompt); err != nil {
		t.Fatalf("CreateMessage msg-2 failed: %v", err)
	}

	messages, err := GetMessagesBySession(db, "sess-1")
	if err != nil {
		t.Fatalf("GetMessagesBySession failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].ID != "msg-1" {
		t.Errorf("expected first message ID 'msg-1', got '%s'", messages[0].ID)
	}
	if messages[0].Content != "Answer from Kimi" {
		t.Errorf("expected first message content 'Answer from Kimi', got '%s'", messages[0].Content)
	}
	if messages[0].Kept != false {
		t.Error("expected new message to have Kept=false by default")
	}
	if messages[1].Error != "timeout" {
		t.Errorf("expected second message error 'timeout', got '%s'", messages[1].Error)
	}
}

func TestGetMessagesBySession_Empty(t *testing.T) {
	db := newTestDB(t)

	messages, err := GetMessagesBySession(db, "nonexistent")
	if err != nil {
		t.Fatalf("GetMessagesBySession failed: %v", err)
	}
	if messages == nil {
		t.Fatal("expected non-nil messages slice")
	}
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestUpdateMessageKept(t *testing.T) {
	db := newTestDB(t)

	if err := CreateSession(db, "sess-1", "Test"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if err := CreateMessage(db, "msg-1", "sess-1", "kimi", "content", "", 1000, 1, "Test"); err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}

	if err := UpdateMessageKept(db, "msg-1", true); err != nil {
		t.Fatalf("UpdateMessageKept failed: %v", err)
	}

	messages, err := GetMessagesBySession(db, "sess-1")
	if err != nil {
		t.Fatalf("GetMessagesBySession failed: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if !messages[0].Kept {
		t.Error("expected message Kept=true after update")
	}

	if err := UpdateMessageKept(db, "msg-1", false); err != nil {
		t.Fatalf("UpdateMessageKept failed: %v", err)
	}

	messages, _ = GetMessagesBySession(db, "sess-1")
	if messages[0].Kept {
		t.Error("expected message Kept=false after second update")
	}
}

func TestFilterKeptBehavior(t *testing.T) {
	db := newTestDB(t)

	if err := CreateSession(db, "sess-1", "Test"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := CreateMessage(db, "msg-1", "sess-1", "kimi", "kept answer", "", 1000, 1, "Test"); err != nil {
		t.Fatalf("CreateMessage msg-1 failed: %v", err)
	}
	if err := CreateMessage(db, "msg-2", "sess-1", "deepseek", "unkept answer", "", 2000, 1, "Test"); err != nil {
		t.Fatalf("CreateMessage msg-2 failed: %v", err)
	}

	if err := UpdateMessageKept(db, "msg-1", true); err != nil {
		t.Fatalf("UpdateMessageKept failed: %v", err)
	}

	allMessages, err := GetMessagesBySession(db, "sess-1")
	if err != nil {
		t.Fatalf("GetMessagesBySession failed: %v", err)
	}
	if len(allMessages) != 2 {
		t.Fatalf("expected 2 total messages, got %d", len(allMessages))
	}

	var keptMessages []models.Message
	for _, msg := range allMessages {
		if msg.Kept {
			keptMessages = append(keptMessages, msg)
		}
	}

	if len(keptMessages) != 1 {
		t.Fatalf("expected 1 kept message, got %d", len(keptMessages))
	}
	if keptMessages[0].ID != "msg-1" {
		t.Errorf("expected kept message ID 'msg-1', got '%s'", keptMessages[0].ID)
	}
}

func TestMessagesOrderedByTurn(t *testing.T) {
	db := newTestDB(t)

	if err := CreateSession(db, "sess-1", "Test"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := CreateMessage(db, "msg-2", "sess-1", "kimi", "turn 2", "", 1000, 2, "Turn 2"); err != nil {
		t.Fatalf("CreateMessage msg-2 failed: %v", err)
	}
	if err := CreateMessage(db, "msg-1", "sess-1", "kimi", "turn 1", "", 1000, 1, "Turn 1"); err != nil {
		t.Fatalf("CreateMessage msg-1 failed: %v", err)
	}

	messages, err := GetMessagesBySession(db, "sess-1")
	if err != nil {
		t.Fatalf("GetMessagesBySession failed: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Turn != 1 {
		t.Errorf("expected first message turn=1, got turn=%d", messages[0].Turn)
	}
	if messages[1].Turn != 2 {
		t.Errorf("expected second message turn=2, got turn=%d", messages[1].Turn)
	}
}

func TestSiteCreateAndGet(t *testing.T) {
	db := newTestDB(t)

	site := models.Site{
		ID:           "test-site",
		Name:         "Test Site",
		URL:          "https://example.com",
		EngineType:   "cdp",
		Selectors:    `{"input":"#input","submit":"#submit","answer":"#answer","wait_for":"#answer"}`,
		Enabled:      true,
		Selected:     true,
		FormatPrompt: "Use markdown",
		CreatedAt:    time.Now(),
	}

	if err := SaveSite(db, site); err != nil {
		t.Fatalf("SaveSite failed: %v", err)
	}

	sites, err := GetSites(db)
	if err != nil {
		t.Fatalf("GetSites failed: %v", err)
	}
	if len(sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(sites))
	}
	if sites[0].ID != "test-site" {
		t.Errorf("expected site ID 'test-site', got '%s'", sites[0].ID)
	}
	if sites[0].FormatPrompt != "Use markdown" {
		t.Errorf("expected format_prompt 'Use markdown', got '%s'", sites[0].FormatPrompt)
	}
	if !sites[0].Selected {
		t.Error("expected site Selected=true")
	}
}

func TestHasSites(t *testing.T) {
	db := newTestDB(t)

	hasSites, err := HasSites(db)
	if err != nil {
		t.Fatalf("HasSites failed: %v", err)
	}
	if hasSites {
		t.Error("expected HasSites=false for empty database")
	}

	site := models.Site{
		ID:         "test-site",
		Name:       "Test Site",
		URL:        "https://example.com",
		EngineType: "cdp",
		Enabled:    true,
		Selected:   true,
	}
	if err := SaveSite(db, site); err != nil {
		t.Fatalf("SaveSite failed: %v", err)
	}

	hasSites, err = HasSites(db)
	if err != nil {
		t.Fatalf("HasSites failed: %v", err)
	}
	if !hasSites {
		t.Error("expected HasSites=true after adding site")
	}
}

func TestUpdateSelected(t *testing.T) {
	db := newTestDB(t)

	site := models.Site{
		ID:         "test-site",
		Name:       "Test Site",
		URL:        "https://example.com",
		EngineType: "cdp",
		Enabled:    true,
		Selected:   true,
	}
	if err := SaveSite(db, site); err != nil {
		t.Fatalf("SaveSite failed: %v", err)
	}

	if err := UpdateSelected(db, "test-site", false); err != nil {
		t.Fatalf("UpdateSelected failed: %v", err)
	}

	sites, err := GetSites(db)
	if err != nil {
		t.Fatalf("GetSites failed: %v", err)
	}
	if sites[0].Selected {
		t.Error("expected site Selected=false after update")
	}

	if err := UpdateSelected(db, "nonexistent", true); err == nil {
		t.Error("expected error when updating nonexistent site")
	}
}

func TestSelectedFieldPersisted(t *testing.T) {
	db := newTestDB(t)

	site := models.Site{
		ID:         "test-site",
		Name:       "Test Site",
		URL:        "https://example.com",
		EngineType: "cdp",
		Enabled:    true,
		Selected:   false,
	}
	if err := SaveSite(db, site); err != nil {
		t.Fatalf("SaveSite failed: %v", err)
	}

	retrieved, err := GetSiteByID(db, "test-site")
	if err != nil {
		t.Fatalf("GetSiteByID failed: %v", err)
	}
	if retrieved.Selected {
		t.Error("expected Selected=false to be persisted")
	}
}
