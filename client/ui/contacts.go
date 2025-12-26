package ui

import (
	"fmt"
	"time"

	"msim-client/protocol"
)

func (a *App) loadContacts() {
	done := make(chan bool, 1)

	handler := func(parts []string) {
		// Format: list|<raw content with contact|nick,...>
		// parts[0] = "list", parts[1] = raw content
		if len(parts) >= 1 && parts[0] == protocol.TypeList {
			content := ""
			if len(parts) >= 2 {
				content = parts[1]
			}
			contacts := protocol.ParseContacts(content)
			a.mu.Lock()
			a.contacts = contacts
			a.mu.Unlock()
			done <- true
		}
	}

	a.client.OnPacket(protocol.TypeList, handler)
	a.client.GetContacts()

	go func() {
		select {
		case <-done:
			a.app.QueueUpdateDraw(func() {
				a.updateContactsList()
			})
		case <-time.After(5 * time.Second):
		}
	}()
}

func (a *App) loadStatuses() {
	handler := func(parts []string) {
		// Format: stat|<raw content with user|status|last_seen,...>
		// parts[0] = "stat", parts[1] = raw content
		if len(parts) >= 1 && parts[0] == protocol.TypeStat {
			content := ""
			if len(parts) >= 2 {
				content = parts[1]
			}
			statuses := protocol.ParseStatuses(content)
			a.mu.Lock()
			for _, s := range statuses {
				a.statuses[s.UserID] = s.Online
				if s.LastSeen != "" {
					a.statusLastSeen[s.UserID] = s.LastSeen
				}
			}
			a.mu.Unlock()
			a.app.QueueUpdateDraw(func() {
				a.updateContactsList()
			})
		}
	}

	a.client.OnPacket(protocol.TypeStat, handler)
	a.client.GetStatus()
}

func (a *App) loadOfflineMessages() {
	handler := func(parts []string) {
		// Format: offmsg|<raw content with contact|count,...>
		// parts[0] = "offmsg", parts[1] = raw content
		if len(parts) >= 1 && parts[0] == protocol.TypeOffmsg {
			content := ""
			if len(parts) >= 2 {
				content = parts[1]
			}
			counts := protocol.ParseOfflineMessages(content)
			a.mu.Lock()
			for _, c := range counts {
				a.unreadCounts[c.ContactID] = c.Count
			}
			a.mu.Unlock()
			a.app.QueueUpdateDraw(func() {
				a.updateContactsList()
			})
		}
	}

	a.client.OnPacket(protocol.TypeOffmsg, handler)
	a.client.GetOfflineMessages()
}

func (a *App) updateContactsList() {
	if a.contactsList == nil {
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()

	currentIdx := a.contactsList.GetCurrentItem()
	a.contactsList.Clear()

	for _, contact := range a.contacts {
		nick := contact.Nick
		if nick == "" {
			nick = contact.ID
		}

		var mainText string
		unread := a.unreadCounts[contact.ID]

		if a.statuses[contact.ID] {
			if unread > 0 {
				mainText = fmt.Sprintf("[green]●[white] %s [gray](%s) [red](%d)", nick, contact.ID, unread)
			} else {
				mainText = fmt.Sprintf("[green]●[white] %s [gray](%s)", nick, contact.ID)
			}
		} else {
			// Format last seen for offline users
			lastSeenStr := ""
			if ts := a.statusLastSeen[contact.ID]; ts != "" {
				if formatted := formatLastSeen(ts); formatted != "" {
					lastSeenStr = fmt.Sprintf(" [gray]— %s", formatted)
				}
			}

			if unread > 0 {
				mainText = fmt.Sprintf("[gray]○[white] %s [gray](%s)%s [red](%d)", nick, contact.ID, lastSeenStr, unread)
			} else {
				mainText = fmt.Sprintf("[gray]○[white] %s [gray](%s)%s", nick, contact.ID, lastSeenStr)
			}
		}

		a.contactsList.AddItem(mainText, "", 0, nil)
	}

	if currentIdx >= 0 && currentIdx < a.contactsList.GetItemCount() {
		a.contactsList.SetCurrentItem(currentIdx)
	}
}
