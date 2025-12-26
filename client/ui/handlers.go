package ui

import (
	"time"

	"msim-client/protocol"
)

func (a *App) setupHandlers() {
	// Handle incoming messages
	a.client.OnPacket(protocol.TypeMsg, func(parts []string) {
		// Format: msg|sender|text|timestamp
		if len(parts) >= 4 {
			sender := parts[1]
			text := parts[2]
			timestamp := parts[3]

			// Send ack
			a.client.SendAck(sender, timestamp)

			// Check if sender is in contacts
			a.mu.RLock()
			isKnown := false
			for _, c := range a.contacts {
				if c.ID == sender {
					isKnown = true
					break
				}
			}
			currentChat := a.currentChat
			a.mu.RUnlock()

			// Add unknown sender to contacts
			if !isKnown {
				a.client.AddContact(sender)
				// Refresh contacts and statuses after adding
				go func() {
					time.Sleep(200 * time.Millisecond)
					a.loadContacts()
					a.loadStatuses()
				}()
			}

			// Store message
			a.mu.Lock()
			a.messages[sender] = append(a.messages[sender], protocol.Message{
				Sender:    sender,
				Text:      text,
				Timestamp: timestamp,
				Status:    "ackn",
			})
			// Increment unread count if not in current chat with this sender
			if currentChat != sender {
				a.unreadCounts[sender]++
			}
			a.mu.Unlock()

			// Update UI if chat is open
			a.app.QueueUpdateDraw(func() {
				if a.currentChat == sender && a.chatView != nil {
					a.refreshChatView()
				}
				a.updateContactsList()
			})
		}
	})

	// Handle ack
	a.client.OnPacket(protocol.TypeAck, func(parts []string) {
		// Format: ack|recipient|timestamp
		if len(parts) >= 3 {
			recipient := parts[1]
			timestamp := parts[2]

			// Update message status
			a.mu.Lock()
			for i, msg := range a.messages[recipient] {
				if msg.Timestamp == timestamp {
					a.messages[recipient][i].Status = "ackn"
					break
				}
			}
			a.mu.Unlock()

			// Update UI
			a.app.QueueUpdateDraw(func() {
				if a.currentChat == recipient && a.chatView != nil {
					a.refreshChatView()
				}
			})
		}
	})

	// Handle online status
	a.client.OnPacket(protocol.TypeOn, func(parts []string) {
		if len(parts) >= 2 {
			userID := parts[1]
			a.mu.Lock()
			a.statuses[userID] = true
			if len(parts) >= 3 {
				a.statusLastSeen[userID] = parts[2]
			}
			a.mu.Unlock()
			a.app.QueueUpdateDraw(func() {
				a.updateContactsList()
				if a.currentChat == userID {
					a.updateChatTitle()
				}
			})
		}
	})

	// Handle offline status
	a.client.OnPacket(protocol.TypeOff, func(parts []string) {
		if len(parts) >= 2 {
			userID := parts[1]
			a.mu.Lock()
			a.statuses[userID] = false
			if len(parts) >= 3 {
				a.statusLastSeen[userID] = parts[2]
			}
			a.mu.Unlock()
			a.app.QueueUpdateDraw(func() {
				a.updateContactsList()
				if a.currentChat == userID {
					a.updateChatTitle()
				}
			})
		}
	})

	// Handle bye from server
	a.client.OnPacket(protocol.TypeBye, func(parts []string) {
		reason := ""
		details := ""
		if len(parts) >= 2 {
			reason = parts[1]
		}
		if len(parts) >= 3 {
			details = parts[2]
		}
		a.client = nil
		a.resetAllStatuses()
		a.app.QueueUpdateDraw(func() {
			a.updateConnectionStatus()
			a.updateStatusBarText()
			a.updateContactsList()
			a.showDisconnectNotification(reason, details)
		})
	})
}
