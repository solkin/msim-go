package protocol

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// Packet types
const (
	TypePing   = "ping"
	TypePong   = "pong"
	TypeBye    = "bye"
	TypeHelp   = "help"
	TypeAuth   = "auth"
	TypeReg    = "reg"
	TypeOk     = "ok"
	TypeFail   = "fail"
	TypeMsg    = "msg"
	TypeAck    = "ack"
	TypeHist   = "hist"
	TypeHClear = "hclear"
	TypeStat   = "stat"
	TypeList   = "list"
	TypeAdd    = "add"
	TypeRen    = "ren"
	TypeDel    = "del"
	TypeOn     = "on"
	TypeOff    = "off"
	TypeOffmsg = "offmsg"
	TypeFsnd   = "fsnd"
	TypeFacc   = "facc"
	TypeFdec   = "fdec"
	TypeFcan   = "fcan"
	TypeFst    = "fst"
)

// Contact represents a contact with id and nickname
type Contact struct {
	ID   string
	Nick string
}

// Message represents a chat message
type Message struct {
	Sender    string
	Text      string
	Timestamp string
	Status    string // "sent" or "ackn"
}

// Status represents user online status
type Status struct {
	UserID   string
	Online   bool
	LastSeen string // ISO 8601 timestamp of last status change
}

// Client represents an mSIM protocol client
type Client struct {
	conn       net.Conn
	reader     *bufio.Reader
	mu         sync.Mutex
	sendMu     sync.Mutex
	handlers   map[string][]func([]string)
	pingTicker *time.Ticker
	done       chan struct{}
	connected  bool
	lastPong   time.Time
	pongMu     sync.RWMutex
}

// NewClient creates a new mSIM client
func NewClient() *Client {
	return &Client{
		handlers: make(map[string][]func([]string)),
		done:     make(chan struct{}),
	}
}

// Connect connects to the mSIM server
func (c *Client) Connect(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.connected = true
	c.lastPong = time.Now()

	// Handle pong to track last response time
	c.OnPacket(TypePong, func(parts []string) {
		c.pongMu.Lock()
		c.lastPong = time.Now()
		c.pongMu.Unlock()
	})

	// Start ping goroutine
	c.pingTicker = time.NewTicker(30 * time.Second)
	go c.pingLoop()

	// Start read goroutine
	go c.readLoop()

	return nil
}

// Disconnect gracefully disconnects from the server
func (c *Client) Disconnect() error {
	if !c.connected {
		return nil
	}
	c.connected = false
	close(c.done)
	if c.pingTicker != nil {
		c.pingTicker.Stop()
	}
	c.Send(TypeBye)
	time.Sleep(100 * time.Millisecond)
	return c.conn.Close()
}

// IsConnected returns connection status
func (c *Client) IsConnected() bool {
	return c.connected
}

// LastPongTime returns time since last pong response
func (c *Client) LastPongTime() time.Duration {
	c.pongMu.RLock()
	defer c.pongMu.RUnlock()
	return time.Since(c.lastPong)
}

// LastPongAt returns the timestamp of last pong
func (c *Client) LastPongAt() time.Time {
	c.pongMu.RLock()
	defer c.pongMu.RUnlock()
	return c.lastPong
}

// pingLoop sends periodic pings
func (c *Client) pingLoop() {
	for {
		select {
		case <-c.done:
			return
		case <-c.pingTicker.C:
			if c.connected {
				c.Send(TypePing)
			}
		}
	}
}

// readLoop reads packets from server
func (c *Client) readLoop() {
	for c.connected {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			if c.connected {
				c.connected = false
				c.notifyHandlers(TypeBye, []string{"connection_lost", ""})
			}
			return
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}

		// For packets with raw content (hist, stat, list), use limited splitting
		// to preserve unescaped pipes in the content
		var parts []string
		if strings.HasPrefix(line, TypeHist+"|") {
			// hist|contact|<raw content with unescaped pipes>
			parts = splitPacketN(line, 3)
		} else if strings.HasPrefix(line, TypeStat+"|") || strings.HasPrefix(line, TypeList+"|") || strings.HasPrefix(line, TypeOffmsg+"|") {
			// stat|<raw content> or list|<raw content> or offmsg|<raw content>
			parts = splitPacketN(line, 2)
		} else {
			parts = splitPacket(line)
		}

		if len(parts) == 0 {
			continue
		}

		packetType := parts[0]
		c.notifyHandlers(packetType, parts)
	}
}

// notifyHandlers notifies registered handlers
func (c *Client) notifyHandlers(packetType string, parts []string) {
	c.mu.Lock()
	handlers := c.handlers[packetType]
	c.mu.Unlock()

	for _, h := range handlers {
		go h(parts)
	}
}

// OnPacket registers a handler for a packet type
func (c *Client) OnPacket(packetType string, handler func([]string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[packetType] = append(c.handlers[packetType], handler)
}

// Send sends a packet to the server
func (c *Client) Send(parts ...string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	if !c.connected && parts[0] != TypeBye {
		return fmt.Errorf("not connected")
	}

	escaped := make([]string, len(parts))
	for i, p := range parts {
		escaped[i] = Escape(p)
	}
	line := strings.Join(escaped, "|") + "\n"
	_, err := c.conn.Write([]byte(line))
	return err
}

// Auth sends authentication request
func (c *Client) Auth(login, password string) error {
	return c.Send(TypeAuth, login, password)
}

// Register sends registration request
func (c *Client) Register(login, password string) error {
	return c.Send(TypeReg, login, password)
}

// SendMessage sends a message to a recipient
func (c *Client) SendMessage(recipient, text string) error {
	return c.Send(TypeMsg, recipient, text)
}

// SendAck sends delivery acknowledgment
func (c *Client) SendAck(sender, timestamp string) error {
	return c.Send(TypeAck, sender, timestamp)
}

// GetHistory requests message history
func (c *Client) GetHistory(contact string) error {
	return c.Send(TypeHist, contact)
}

// ClearHistory clears message history with a contact
func (c *Client) ClearHistory(contact string) error {
	return c.Send(TypeHClear, contact)
}

// GetStatus requests status of all contacts or specific user
func (c *Client) GetStatus(userID ...string) error {
	if len(userID) > 0 {
		return c.Send(TypeStat, userID[0])
	}
	return c.Send(TypeStat)
}

// GetContacts requests contact list
func (c *Client) GetContacts() error {
	return c.Send(TypeList)
}

// AddContact adds a new contact
func (c *Client) AddContact(id string, nick ...string) error {
	if len(nick) > 0 {
		return c.Send(TypeAdd, id, nick[0])
	}
	return c.Send(TypeAdd, id)
}

// RenameContact renames a contact
func (c *Client) RenameContact(id, newNick string) error {
	return c.Send(TypeRen, id, newNick)
}

// DeleteContact deletes a contact
func (c *Client) DeleteContact(id string) error {
	return c.Send(TypeDel, id)
}

// Escape escapes special characters in a string
func Escape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}

// Unescape unescapes special characters in a string
func Unescape(s string) string {
	var result strings.Builder
	escaped := false
	for _, ch := range s {
		if escaped {
			switch ch {
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
				result.WriteRune('\\')
				result.WriteRune(ch)
			}
			escaped = false
		} else if ch == '\\' {
			escaped = true
		} else {
			result.WriteRune(ch)
		}
	}
	if escaped {
		result.WriteRune('\\')
	}
	return result.String()
}

// splitPacket splits a packet by unescaped pipe
func splitPacket(line string) []string {
	var parts []string
	var current strings.Builder
	escaped := false

	for _, ch := range line {
		if escaped {
			current.WriteRune('\\')
			current.WriteRune(ch)
			escaped = false
		} else if ch == '\\' {
			escaped = true
		} else if ch == '|' {
			parts = append(parts, Unescape(current.String()))
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	parts = append(parts, Unescape(current.String()))
	return parts
}

// splitPacketN splits a packet by unescaped pipe, but only up to n parts
// The last part contains the remaining content (may contain unescaped pipes)
func splitPacketN(line string, n int) []string {
	if n <= 0 {
		return []string{line}
	}

	var parts []string
	var current strings.Builder
	escaped := false
	count := 0
	bytePos := 0

	for _, ch := range line {
		charLen := len(string(ch))

		if count >= n-1 {
			// Return the rest as the last part (don't unescape - it may have structural pipes)
			parts = append(parts, line[bytePos:])
			return parts
		}

		if escaped {
			current.WriteRune('\\')
			current.WriteRune(ch)
			escaped = false
		} else if ch == '\\' {
			escaped = true
		} else if ch == '|' {
			parts = append(parts, Unescape(current.String()))
			current.Reset()
			count++
		} else {
			current.WriteRune(ch)
		}
		bytePos += charLen
	}
	if escaped {
		current.WriteRune('\\')
	}
	parts = append(parts, Unescape(current.String()))
	return parts
}

// SplitList splits a comma-separated list
func SplitList(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	var current strings.Builder
	escaped := false

	for _, ch := range s {
		if escaped {
			current.WriteRune('\\')
			current.WriteRune(ch)
			escaped = false
		} else if ch == '\\' {
			escaped = true
		} else if ch == ',' {
			parts = append(parts, current.String())
			current.Reset()
		} else {
			current.WriteRune(ch)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	parts = append(parts, current.String())
	return parts
}

// ParseContacts parses contact list response
func ParseContacts(content string) []Contact {
	if content == "" {
		return nil
	}
	items := SplitList(content)
	var contacts []Contact
	for _, item := range items {
		parts := splitPacket(item)
		if len(parts) >= 2 {
			contacts = append(contacts, Contact{
				ID:   parts[0],
				Nick: parts[1],
			})
		}
	}
	return contacts
}

// ParseStatuses parses status response
// Format: user|status|last_seen (last_seen is optional for backwards compatibility)
func ParseStatuses(content string) []Status {
	if content == "" {
		return nil
	}
	items := SplitList(content)
	var statuses []Status
	for _, item := range items {
		parts := splitPacket(item)
		if len(parts) >= 2 {
			s := Status{
				UserID: parts[0],
				Online: parts[1] == "on",
			}
			if len(parts) >= 3 {
				s.LastSeen = parts[2]
			}
			statuses = append(statuses, s)
		}
	}
	return statuses
}

// OfflineMessageCount represents count of offline messages from a contact
type OfflineMessageCount struct {
	ContactID string
	Count     int
}

// ParseOfflineMessages parses offmsg response
// Format: contact|count,contact|count,...
func ParseOfflineMessages(content string) []OfflineMessageCount {
	if content == "" {
		return nil
	}
	items := SplitList(content)
	var counts []OfflineMessageCount
	for _, item := range items {
		parts := splitPacket(item)
		if len(parts) >= 2 {
			count := 0
			fmt.Sscanf(parts[1], "%d", &count)
			if count > 0 {
				counts = append(counts, OfflineMessageCount{
					ContactID: parts[0],
					Count:     count,
				})
			}
		}
	}
	return counts
}

// GetOfflineMessages requests offline messages count
func (c *Client) GetOfflineMessages() error {
	return c.Send(TypeOffmsg)
}

// SendFile initiates a file transfer
// Format: fsnd|recipient|filename|size|hash
func (c *Client) SendFile(recipient, filename string, size int64, hash string) error {
	return c.Send(TypeFsnd, recipient, filename, fmt.Sprintf("%d", size), hash)
}

// AcceptFile accepts a file transfer
// Format: facc|sender|session_id
func (c *Client) AcceptFile(sender, sessionID string) error {
	return c.Send(TypeFacc, sender, sessionID)
}

// DeclineFile declines a file transfer
// Format: fdec|sender|session_id|reason
func (c *Client) DeclineFile(sender, sessionID, reason string) error {
	return c.Send(TypeFdec, sender, sessionID, reason)
}

// CancelFile cancels a file transfer
// Format: fcan|user|session_id|reason
func (c *Client) CancelFile(user, sessionID, reason string) error {
	return c.Send(TypeFcan, user, sessionID, reason)
}

// GetFileStatus requests file transfer status
// Format: fst|session_id
func (c *Client) GetFileStatus(sessionID string) error {
	return c.Send(TypeFst, sessionID)
}

// GetServerAddr returns the server address for file transfer connections
func (c *Client) GetServerAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().(*net.TCPAddr).IP.String()
}

// ParseHistory parses history response content
// Format: msg|sender|text|timestamp|status,msg|sender|text|timestamp|status,...
func ParseHistory(content string) []Message {
	if content == "" {
		return nil
	}
	items := SplitList(content)
	var messages []Message
	for _, item := range items {
		// Use splitPacketN with 5 parts to handle text that may contain |
		// Format: msg|sender|text|timestamp|status
		parts := splitPacketN(item, 5)
		if len(parts) >= 5 && parts[0] == TypeMsg {
			messages = append(messages, Message{
				Sender:    parts[1],
				Text:      parts[2],
				Timestamp: parts[3],
				Status:    parts[4],
			})
		}
	}
	return messages
}

// ParseHistoryFromParts is deprecated, use ParseHistory with raw content
func ParseHistoryFromParts(parts []string) []Message {
	// This is called with parts after hist|contact| have been stripped
	// Join them back and parse
	content := strings.Join(parts, "|")
	return ParseHistory(content)
}
