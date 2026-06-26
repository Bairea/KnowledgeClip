package export

import (
	"fmt"
	"strings"

	"chat-aggregator/internal/models"
)

func ToMarkdown(session models.Session, messages []models.Message, sites []models.Site) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s\n\n", session.Prompt))

	siteMap := make(map[string]string)
	for _, site := range sites {
		siteMap[site.ID] = site.Name
	}

	for _, msg := range messages {
		siteName := siteMap[msg.SiteID]
		if siteName == "" {
			siteName = msg.SiteID
		}

		b.WriteString(fmt.Sprintf("## %s\n\n", siteName))

		if msg.Error != "" {
			b.WriteString(fmt.Sprintf("_Error: %s_\n\n", msg.Error))
		} else if msg.Content != "" {
			b.WriteString(offsetHeadings(msg.Content, 2))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func offsetHeadings(content string, offset int) string {
	lines := strings.Split(content, "\n")
	inCodeBlock := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}

		if inCodeBlock {
			continue
		}

		level := countHeadingLevel(line)
		if level > 0 {
			newLevel := level + offset
			if newLevel > 6 {
				newLevel = 6
			}
			lines[i] = strings.Repeat("#", newLevel) + line[level:]
		}
	}

	return strings.Join(lines, "\n")
}

func countHeadingLevel(line string) int {
	count := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '#' {
			count++
		} else {
			break
		}
	}
	if count > 0 && count <= 6 && count < len(line) && line[count] == ' ' {
		return count
	}
	return 0
}
