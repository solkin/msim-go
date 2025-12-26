package ui

import (
	"fmt"
	"time"
)

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "just now"
	}
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes = minutes % 60
	return fmt.Sprintf("%dh %dm", hours, minutes)
}

// formatLastSeen formats the last seen timestamp for display
func formatLastSeen(timestamp string) string {
	if timestamp == "" {
		return ""
	}

	// Parse ISO 8601 timestamp
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ""
	}

	now := time.Now()
	diff := now.Sub(t)

	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d min ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if diff < 30*24*time.Hour {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else {
		return t.Format("Jan 2, 2006")
	}
}

// formatDateSeparator formats a date for display as a separator
func formatDateSeparator(timestamp string) string {
	if len(timestamp) < 10 {
		return ""
	}

	// Parse the date part (YYYY-MM-DD)
	t, err := time.Parse("2006-01-02", timestamp[:10])
	if err != nil {
		return timestamp[:10]
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterday := today.AddDate(0, 0, -1)
	msgDate := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())

	if msgDate.Equal(today) {
		return "Today"
	} else if msgDate.Equal(yesterday) {
		return "Yesterday"
	} else if msgDate.Year() == now.Year() {
		return t.Format("January 2")
	} else {
		return t.Format("January 2, 2006")
	}
}
