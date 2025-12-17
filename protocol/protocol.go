package protocol

import (
	"errors"
	"strings"
)

var (
	ErrInvalidPacket = errors.New("invalid packet format")
)

type Packet struct {
	Type        string
	Destination string
	Content     string
	Fields      []string // разобранные поля из Content
}

func ParsePacket(line string) (*Packet, error) {
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")

	parts := splitUnescaped(line, '|')
	if len(parts) < 1 {
		return nil, ErrInvalidPacket
	}

	pkt := &Packet{
		Type: unescape(parts[0]),
	}

	if len(parts) == 2 {
		// TYPE|CONTENT (для сервера)
		pkt.Content = unescape(parts[1])
		pkt.Fields = splitUnescaped(pkt.Content, '|')
	} else if len(parts) >= 3 {
		// TYPE|DESTINATION|CONTENT
		pkt.Destination = unescape(parts[1])
		pkt.Content = unescape(parts[2])
		pkt.Fields = splitUnescaped(pkt.Content, '|')
	}

	return pkt, nil
}

func FormatPacket(pktType string, destination string, content string) string {
	var parts []string
	parts = append(parts, Escape(pktType))

	if destination != "" {
		parts = append(parts, Escape(destination))
	}

	if content != "" {
		parts = append(parts, Escape(content))
	}

	return strings.Join(parts, "|") + "\n"
}

func FormatSimplePacket(pktType string, content string) string {
	return FormatPacket(pktType, "", content)
}

func FormatListPacket(pktType string, items []string) string {
	return FormatSimplePacket(pktType, strings.Join(items, ","))
}

// splitUnescaped разбивает строку по разделителю, игнорируя экранированные символы
func splitUnescaped(s string, delimiter rune) []string {
	var parts []string
	var current strings.Builder
	escape := false

	for _, r := range s {
		if escape {
			current.WriteRune(r)
			escape = false
			continue
		}

		if r == '\\' {
			escape = true
			current.WriteRune(r)
			continue
		}

		if r == delimiter {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}

		current.WriteRune(r)
	}

	parts = append(parts, current.String())
	return parts
}

// unescape раскодирует экранированные символы
func unescape(s string) string {
	var result strings.Builder
	escape := false

	for i, r := range s {
		if escape {
			switch r {
			case '|':
				result.WriteRune('|')
			case ',':
				result.WriteRune(',')
			case '\\':
				result.WriteRune('\\')
			case 'n':
				result.WriteRune('\n')
			case 'r':
				result.WriteRune('\r')
			default:
				// Если экранирование не распознано, оставляем как есть
				result.WriteRune('\\')
				result.WriteRune(r)
			}
			escape = false
			continue
		}

		if r == '\\' {
			// Проверяем, не последний ли это символ
			if i < len(s)-1 {
				escape = true
				continue
			}
		}

		result.WriteRune(r)
	}

	// Если строка заканчивается на неэкранированный обратный слэш
	if escape {
		result.WriteRune('\\')
	}

	return result.String()
}

// Escape экранирует специальные символы
func Escape(s string) string {
	var result strings.Builder

	for _, r := range s {
		switch r {
		case '|':
			result.WriteString("\\|")
		case ',':
			result.WriteString("\\,")
		case '\\':
			result.WriteString("\\\\")
		case '\n':
			result.WriteString("\\n")
		case '\r':
			result.WriteString("\\r")
		default:
			result.WriteRune(r)
		}
	}

	return result.String()
}
