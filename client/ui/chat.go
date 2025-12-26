package ui

import (
	"fmt"
	"strings"
	"time"

	"msim-client/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func (a *App) openChat(contactID string) {
	a.mu.Lock()
	a.currentChat = contactID
	// Store unread count for marker calculation after history loads
	a.pendingUnreadCount = a.unreadCounts[contactID]
	a.unreadMarker = -1 // Will be calculated after history loads
	// Reset unread count when opening chat
	a.unreadCounts[contactID] = 0
	a.mu.Unlock()

	chatPage := a.createChatPage(contactID)
	a.pages.AddPage("chat", chatPage, true, true)
	a.pages.SwitchToPage("chat")

	// Update contacts list to reflect cleared unread count
	a.updateContactsList()

	// Load history
	a.loadHistory(contactID)
}

func (a *App) getChatTitle(contactID string) string {
	a.mu.RLock()
	online := a.statuses[contactID]
	nick := contactID
	for _, c := range a.contacts {
		if c.ID == contactID && c.Nick != "" {
			nick = c.Nick
			break
		}
	}
	a.mu.RUnlock()

	status := "○ offline"
	if online {
		status = "● online"
	}
	return fmt.Sprintf(" %s ─ %s ", nick, status)
}

func (a *App) updateChatTitle() {
	if a.chatView != nil && a.currentChat != "" {
		a.chatView.SetTitle(a.getChatTitle(a.currentChat))
	}
}

func (a *App) createChatPage(contactID string) tview.Primitive {
	// Chat history view
	a.chatView = tview.NewTextView()
	a.chatView.SetBorder(true)
	a.chatView.SetBorderColor(ColorBorder)
	a.chatView.SetBackgroundColor(ColorBg)
	a.chatView.SetTitle(a.getChatTitle(contactID))
	a.chatView.SetTitleColor(ColorTitle)
	a.chatView.SetTextColor(ColorFg)
	a.chatView.SetDynamicColors(true)
	a.chatView.SetScrollable(true)
	a.chatView.ScrollToEnd()

	// Message input
	a.messageInput = tview.NewInputField()
	a.messageInput.SetLabel("> ")
	a.messageInput.SetFieldWidth(0)
	a.messageInput.SetBackgroundColor(ColorBg)
	a.messageInput.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	a.messageInput.SetFieldTextColor(ColorFg)
	a.messageInput.SetLabelColor(ColorHighlight)
	a.messageInput.SetBorder(true)
	a.messageInput.SetBorderColor(ColorBorder)
	a.messageInput.SetTitle(" Message ")
	a.messageInput.SetTitleColor(ColorTitle)

	a.messageInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			text := a.messageInput.GetText()
			if text != "" {
				a.sendMessage(contactID, text)
				a.messageInput.SetText("")
			}
		}
	})

	// Status bar
	chatStatus := tview.NewTextView()
	chatStatus.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	chatStatus.SetTextColor(ColorTitle)
	chatStatus.SetTextAlign(tview.AlignCenter)
	chatStatus.SetText(" Enter:Send | Tab:Scroll | F5:Refresh | F8:Clear | Esc:Back ")

	// Layout
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.chatView, 0, 1, false).
		AddItem(a.messageInput, 3, 0, true).
		AddItem(chatStatus, 1, 0, false)
	mainFlex.SetBackgroundColor(ColorBg)

	// Track focus on chat view for scrolling
	chatViewFocused := false

	// Handle keyboard
	mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			if chatViewFocused {
				chatViewFocused = false
				a.app.SetFocus(a.messageInput)
				chatStatus.SetText(" Enter:Send | Tab:Scroll | F5:Refresh | F8:Clear | Esc:Back ")
				return nil
			}
			a.closeChat()
			return nil
		case tcell.KeyTab:
			chatViewFocused = !chatViewFocused
			if chatViewFocused {
				a.app.SetFocus(a.chatView)
				chatStatus.SetText(" ↑↓/PgUp/PgDn:Scroll | Home:Top | End:Bottom | Tab/Esc:Input ")
			} else {
				a.app.SetFocus(a.messageInput)
				chatStatus.SetText(" Enter:Send | Tab:Scroll | F5:Refresh | F8:Clear | Esc:Back ")
			}
			return nil
		case tcell.KeyF5:
			a.loadHistory(contactID)
			return nil
		case tcell.KeyF8:
			a.showClearHistoryDialog(contactID)
			return nil
		case tcell.KeyPgUp:
			row, col := a.chatView.GetScrollOffset()
			a.chatView.ScrollTo(row-10, col)
			return nil
		case tcell.KeyPgDn:
			row, col := a.chatView.GetScrollOffset()
			a.chatView.ScrollTo(row+10, col)
			return nil
		case tcell.KeyUp:
			if chatViewFocused {
				row, col := a.chatView.GetScrollOffset()
				a.chatView.ScrollTo(row-1, col)
				return nil
			}
		case tcell.KeyDown:
			if chatViewFocused {
				row, col := a.chatView.GetScrollOffset()
				a.chatView.ScrollTo(row+1, col)
				return nil
			}
		case tcell.KeyHome:
			if chatViewFocused {
				a.chatView.ScrollToBeginning()
				return nil
			}
		case tcell.KeyEnd:
			if chatViewFocused {
				a.chatView.ScrollToEnd()
				return nil
			}
		}
		return event
	})

	return mainFlex
}

func (a *App) loadHistory(contactID string) {
	handler := func(parts []string) {
		// Format: hist|contact|<raw content with msg|sender|text|timestamp|status,...>
		// parts[0] = "hist", parts[1] = "contact", parts[2] = raw content
		if len(parts) >= 2 && parts[0] == protocol.TypeHist {
			content := ""
			if len(parts) >= 3 {
				content = parts[2]
			}
			messages := protocol.ParseHistory(content)
			a.mu.Lock()
			a.messages[contactID] = messages
			// Calculate unread marker position from pending unread count (only once)
			// Only process if there's a pending count - don't reset marker if already set
			unreadCount := a.pendingUnreadCount
			if unreadCount > 0 {
				if unreadCount <= len(messages) {
					a.unreadMarker = len(messages) - unreadCount
				} else {
					// More unreads than messages - show marker at beginning
					a.unreadMarker = 0
				}
				// Reset pending count only after processing
				a.pendingUnreadCount = 0
			}
			// If pendingUnreadCount was 0, don't touch unreadMarker (might be set by previous handler)
			a.mu.Unlock()
			a.app.QueueUpdateDraw(func() {
				a.refreshChatView()
			})
		}
	}

	a.client.OnPacket(protocol.TypeHist, handler)
	a.client.GetHistory(contactID)
}

func (a *App) refreshChatView() {
	if a.chatView == nil {
		return
	}

	a.mu.RLock()
	messages := a.messages[a.currentChat]
	unreadMarker := a.unreadMarker
	a.mu.RUnlock()

	// Get chat view width for full-width separator
	_, _, width, _ := a.chatView.GetInnerRect()
	if width < 10 {
		width = 80 // Default width
	}

	a.chatView.Clear()
	var sb strings.Builder
	var lastDate string

	for i, msg := range messages {
		// Extract date from timestamp (YYYY-MM-DD)
		msgDate := ""
		if len(msg.Timestamp) >= 10 {
			msgDate = msg.Timestamp[:10]
		}

		// Insert date separator when date changes
		if msgDate != "" && msgDate != lastDate {
			dateLabel := formatDateSeparator(msg.Timestamp)
			// Center the date label
			padding := (width - len(dateLabel)) / 2
			if padding < 0 {
				padding = 0
			}
			sb.WriteString(fmt.Sprintf("[gray]%s%s[-]\n", strings.Repeat(" ", padding), dateLabel))
			lastDate = msgDate
		}

		// Insert unread marker before unread messages
		if unreadMarker >= 0 && i == unreadMarker {
			// Create full-width separator with "Unread" in the middle
			label := " Unread "
			sideLen := (width - len(label)) / 2
			if sideLen < 1 {
				sideLen = 1
			}
			leftSide := strings.Repeat("─", sideLen)
			rightSide := strings.Repeat("─", width-sideLen-len(label))
			sb.WriteString(fmt.Sprintf("[red]%s%s%s[-]\n", leftSide, label, rightSide))
		}

		timeStr := ""
		if len(msg.Timestamp) >= 19 {
			timeStr = msg.Timestamp[11:19] // Extract HH:MM:SS
		} else {
			timeStr = msg.Timestamp
		}

		statusIcon := "[gray]○[-]" // sent
		if msg.Status == "ackn" {
			statusIcon = "[green]✓[-]"
		}

		// Outgoing = white, Incoming = yellow
		if msg.Sender == a.currentUser {
			sb.WriteString(fmt.Sprintf("[gray]%s[-] [white]→ %s[-] %s\n",
				timeStr, msg.Text, statusIcon))
		} else {
			sb.WriteString(fmt.Sprintf("[gray]%s[-] [yellow]← %s[-] %s\n",
				timeStr, msg.Text, statusIcon))
		}
	}

	a.chatView.SetText(sb.String())
	a.chatView.ScrollToEnd()
}

func (a *App) sendMessage(contactID, text string) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	// Store message locally with sent status
	a.mu.Lock()
	a.messages[contactID] = append(a.messages[contactID], protocol.Message{
		Sender:    a.currentUser,
		Text:      text,
		Timestamp: timestamp,
		Status:    "sent",
	})
	a.mu.Unlock()

	// Send to server
	a.client.SendMessage(contactID, text)

	// Update view
	a.refreshChatView()
}

func (a *App) closeChat() {
	a.mu.Lock()
	a.currentChat = ""
	a.unreadMarker = -1 // Reset marker when closing chat
	a.mu.Unlock()
	a.chatView = nil
	a.messageInput = nil
	a.pages.RemovePage("chat")
	a.pages.SwitchToPage("main")
	a.app.SetFocus(a.contactsList)
}
