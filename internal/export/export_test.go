package export

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"chat-aggregator/internal/models"
)

func makeSession() models.Session {
	return models.Session{
		ID:        "test-session-1",
		Prompt:    "What is Python?",
		CreatedAt: time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC),
	}
}

func makeMessages() []models.Message {
	return []models.Message{
		{
			ID:        "msg-1",
			SessionID: "test-session-1",
			SiteID:    "kimi",
			Content:   "### Python Overview\n\nPython is a high-level language.\n\n```python\nprint('hello')\n```",
			Kept:      true,
			ElapsedMs: 5000,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Date(2026, 6, 26, 12, 0, 5, 0, time.UTC),
		},
		{
			ID:        "msg-2",
			SessionID: "test-session-1",
			SiteID:    "deepseek",
			Content:   "### Python Basics\n\nPython supports multiple paradigms.",
			Kept:      false,
			ElapsedMs: 3000,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Date(2026, 6, 26, 12, 0, 3, 0, time.UTC),
		},
		{
			ID:        "msg-3",
			SessionID: "test-session-1",
			SiteID:    "qwen",
			Content:   "",
			Kept:      false,
			Error:     "timeout",
			ElapsedMs: 60000,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Date(2026, 6, 26, 12, 1, 0, 0, time.UTC),
		},
	}
}

func makeSites() []models.Site {
	return []models.Site{
		{ID: "kimi", Name: "Kimi"},
		{ID: "deepseek", Name: "DeepSeek"},
		{ID: "qwen", Name: "Qwen"},
	}
}

func TestToMarkdown_WithMessages(t *testing.T) {
	session := makeSession()
	messages := makeMessages()
	sites := makeSites()

	result := ToMarkdown(session, messages, sites)

	if !strings.HasPrefix(result, "# What is Python?") {
		t.Errorf("expected markdown to start with session prompt as H1, got: %s", result[:min(50, len(result))])
	}

	if !strings.Contains(result, "## Kimi") {
		t.Error("expected markdown to contain '## Kimi' section header")
	}

	if !strings.Contains(result, "## DeepSeek") {
		t.Error("expected markdown to contain '## DeepSeek' section header")
	}

	if !strings.Contains(result, "## Qwen") {
		t.Error("expected markdown to contain '## Qwen' section header")
	}

	if !strings.Contains(result, "Python is a high-level language.") {
		t.Error("expected markdown to contain Kimi message content")
	}

	if !strings.Contains(result, "Python supports multiple paradigms.") {
		t.Error("expected markdown to contain DeepSeek message content")
	}

	if !strings.Contains(result, "_Error: timeout_") {
		t.Error("expected markdown to contain error message for Qwen")
	}
}

func TestToMarkdown_HeadingOffset(t *testing.T) {
	session := makeSession()
	messages := []models.Message{
		{
			ID:        "msg-1",
			SessionID: "test-session-1",
			SiteID:    "kimi",
			Content:   "### Python Overview\n\nSome text.\n\n#### Subsection\n\nMore text.",
			Kept:      true,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Now(),
		},
	}
	sites := makeSites()

	result := ToMarkdown(session, messages, sites)

	if !strings.Contains(result, "##### Python Overview") {
		t.Error("expected H3 to be offset to H5 (offset=2), checking '##### Python Overview'")
	}

	if !strings.Contains(result, "###### Subsection") {
		t.Error("expected H4 to be offset to H6 (offset=2), checking '###### Subsection'")
	}
}

func TestToMarkdown_HeadingOffsetInCodeBlock(t *testing.T) {
	session := makeSession()
	messages := []models.Message{
		{
			ID:        "msg-1",
			SessionID: "test-session-1",
			SiteID:    "kimi",
			Content:   "### Title\n\n```markdown\n# Not a real heading\n```\n\nText after.",
			Kept:      true,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Now(),
		},
	}
	sites := makeSites()

	result := ToMarkdown(session, messages, sites)

	if !strings.Contains(result, "# Not a real heading") {
		t.Error("heading inside code block should remain unchanged")
	}

	if strings.Contains(result, "### Not a real heading") {
		t.Error("heading inside code block should not be offset to ###")
	}

	if !strings.Contains(result, "##### Title") {
		t.Error("real H3 heading should be offset to H5")
	}
}

func TestToMarkdown_EmptyMessages(t *testing.T) {
	session := makeSession()
	messages := []models.Message{}
	sites := makeSites()

	result := ToMarkdown(session, messages, sites)

	if !strings.HasPrefix(result, "# What is Python?") {
		t.Errorf("expected markdown to start with session prompt even with no messages")
	}

	sections := strings.Count(result, "## ")
	if sections != 0 {
		t.Errorf("expected 0 site sections with empty messages, got %d", sections)
	}
}

func TestToMarkdown_UnknownSiteID(t *testing.T) {
	session := makeSession()
	messages := []models.Message{
		{
			ID:        "msg-1",
			SessionID: "test-session-1",
			SiteID:    "unknown-site",
			Content:   "Content from unknown site.",
			Kept:      true,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Now(),
		},
	}
	sites := makeSites()

	result := ToMarkdown(session, messages, sites)

	if !strings.Contains(result, "## unknown-site") {
		t.Error("expected unknown site ID to be used as section header when site not found")
	}
}

func TestToJSON_WithMessages(t *testing.T) {
	session := makeSession()
	messages := makeMessages()

	data, err := ToJSON(session, messages)
	if err != nil {
		t.Fatalf("ToJSON returned error: %v", err)
	}

	var export SessionExport
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if export.Session.ID != "test-session-1" {
		t.Errorf("expected session ID 'test-session-1', got '%s'", export.Session.ID)
	}

	if export.Session.Prompt != "What is Python?" {
		t.Errorf("expected session prompt 'What is Python?', got '%s'", export.Session.Prompt)
	}

	if len(export.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(export.Messages))
	}

	if export.Messages[0].SiteID != "kimi" {
		t.Errorf("expected first message site 'kimi', got '%s'", export.Messages[0].SiteID)
	}

	if export.Messages[2].Error != "timeout" {
		t.Errorf("expected third message error 'timeout', got '%s'", export.Messages[2].Error)
	}
}

func TestToJSON_EmptyMessages(t *testing.T) {
	session := makeSession()
	messages := []models.Message{}

	data, err := ToJSON(session, messages)
	if err != nil {
		t.Fatalf("ToJSON returned error: %v", err)
	}

	var export SessionExport
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if export.Session.ID != "test-session-1" {
		t.Errorf("expected session ID 'test-session-1', got '%s'", export.Session.ID)
	}

	if len(export.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(export.Messages))
	}
}

func TestOffsetHeadings_BasicOffset(t *testing.T) {
	input := "### Title\n\nSome content.\n\n#### Subtitle"
	result := offsetHeadings(input, 2)

	if !strings.Contains(result, "##### Title") {
		t.Error("expected H3 + 2 = H5")
	}

	if !strings.Contains(result, "###### Subtitle") {
		t.Error("expected H4 + 2 = H6")
	}
}

func TestOffsetHeadings_MaxLevel(t *testing.T) {
	input := "###### Deep Heading"
	result := offsetHeadings(input, 3)

	if !strings.Contains(result, "###### Deep Heading") {
		t.Error("expected H6 + 3 to be capped at H6")
	}
}

func TestOffsetHeadings_NoHeadings(t *testing.T) {
	input := "Just regular text.\n\nNo headings here."
	result := offsetHeadings(input, 2)

	if result != input {
		t.Error("expected no changes when there are no headings")
	}
}

func TestOffsetHeadings_CodeBlockProtection(t *testing.T) {
	input := "### Real Title\n\n```python\n# This is a comment\nx = 1\n```\n\n#### Another Real Title"
	result := offsetHeadings(input, 2)

	if !strings.Contains(result, "##### Real Title") {
		t.Error("expected real H3 to be offset to H5")
	}

	if !strings.Contains(result, "###### Another Real Title") {
		t.Error("expected real H4 to be offset to H6")
	}

	if !strings.Contains(result, "# This is a comment") {
		t.Error("expected comment inside code block to remain unchanged")
	}
}

func TestCountHeadingLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"# H1", 1},
		{"## H2", 2},
		{"### H3", 3},
		{"#### H4", 4},
		{"##### H5", 5},
		{"###### H6", 6},
		{"No heading", 0},
		{"#NoSpace", 0},
		{"", 0},
		{"####### Too many", 0},
		{"Text # not heading", 0},
	}

	for _, tt := range tests {
		result := countHeadingLevel(tt.input)
		if result != tt.expected {
			t.Errorf("countHeadingLevel(%q) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func TestToMarkdown_MultipleTurns(t *testing.T) {
	session := makeSession()
	messages := []models.Message{
		{
			ID:        "msg-1",
			SessionID: "test-session-1",
			SiteID:    "kimi",
			Content:   "### Turn 1 Answer\n\nFirst answer.",
			Kept:      true,
			Turn:      1,
			Prompt:    "What is Python?",
			CreatedAt: time.Now(),
		},
		{
			ID:        "msg-2",
			SessionID: "test-session-1",
			SiteID:    "kimi",
			Content:   "### Turn 2 Answer\n\nSecond answer.",
			Kept:      true,
			Turn:      2,
			Prompt:    "How about Go?",
			CreatedAt: time.Now(),
		},
	}
	sites := makeSites()

	result := ToMarkdown(session, messages, sites)

	if strings.Count(result, "## Kimi") != 2 {
		t.Errorf("expected 2 Kimi sections (one per turn), got %d", strings.Count(result, "## Kimi"))
	}

	if !strings.Contains(result, "First answer.") {
		t.Error("expected first turn answer in markdown")
	}

	if !strings.Contains(result, "Second answer.") {
		t.Error("expected second turn answer in markdown")
	}
}

func TestToJSON_RoundTrip(t *testing.T) {
	session := makeSession()
	messages := makeMessages()

	data, err := ToJSON(session, messages)
	if err != nil {
		t.Fatalf("ToJSON returned error: %v", err)
	}

	var export SessionExport
	if err := json.Unmarshal(data, &export); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	data2, err := ToJSON(export.Session, export.Messages)
	if err != nil {
		t.Fatalf("second ToJSON returned error: %v", err)
	}

	if string(data) != string(data2) {
		t.Error("JSON round-trip should produce identical output")
	}
}
