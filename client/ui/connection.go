package ui

import (
	"fmt"
	"time"

	"msim-client/protocol"
)

func (a *App) updateConnectionStatus() {
	if a.connectionView == nil {
		return
	}
	if a.client != nil && a.client.IsConnected() {
		lastPong := a.client.LastPongTime()
		pingStr := formatDuration(lastPong)
		a.connectionView.SetText(fmt.Sprintf("[green]● Connected to %s[-] [gray]│ Last ping: %s ago[-]", a.serverAddr, pingStr))
	} else {
		a.connectionView.SetText(fmt.Sprintf("[red]○ Disconnected from %s[-]", a.serverAddr))
	}
}

func (a *App) startStatusTicker() {
	if a.statusTicker != nil {
		return
	}
	a.statusTickerDone = make(chan struct{})
	a.statusTicker = time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-a.statusTickerDone:
				return
			case <-a.statusTicker.C:
				if a.client != nil && a.client.IsConnected() {
					a.app.QueueUpdateDraw(func() {
						a.updateConnectionStatus()
						a.updateContactsList() // Refresh last seen times
					})
				}
			}
		}
	}()
}

func (a *App) stopStatusTicker() {
	if a.statusTicker != nil {
		a.statusTicker.Stop()
		close(a.statusTickerDone)
		a.statusTicker = nil
	}
}

func (a *App) setConnectionError(err string) {
	if a.connectionView == nil {
		return
	}
	a.connectionView.SetText(fmt.Sprintf("[red]✗ Error: %s[-]", err))
}

func (a *App) updateStatusBarText() {
	if a.statusBar == nil {
		return
	}
	if a.client != nil && a.client.IsConnected() {
		a.statusBar.SetText(" F1:Help | F2:Add | F3:Rename | F4:Delete | F5:Refresh | F6:Disconnect | F10:Quit ")
	} else {
		a.statusBar.SetText(" F1:Help | F6:Connect | F10:Quit ")
	}
}

func (a *App) resetAllStatuses() {
	a.mu.Lock()
	for k := range a.statuses {
		a.statuses[k] = false
	}
	// Also reset unread counts and last seen on disconnect
	for k := range a.unreadCounts {
		delete(a.unreadCounts, k)
	}
	for k := range a.statusLastSeen {
		delete(a.statusLastSeen, k)
	}
	a.mu.Unlock()
}

func (a *App) toggleConnection() {
	if a.client != nil && a.client.IsConnected() {
		// Disconnect
		a.connectionView.SetText("[yellow]Disconnecting...[-]")
		a.client.Disconnect()
		a.client = nil
		a.resetAllStatuses()
		a.updateConnectionStatus()
		a.updateStatusBarText()
		a.updateContactsList()
	} else {
		// Reconnect
		a.connectionView.SetText("[yellow]Connecting...[-]")
		go a.reconnect()
	}
}

func (a *App) reconnect() {
	a.client = protocol.NewClient()
	err := a.client.Connect(a.serverAddr)
	if err != nil {
		a.app.QueueUpdateDraw(func() {
			a.setConnectionError(fmt.Sprintf("Connection failed: %v", err))
			a.updateStatusBarText()
		})
		return
	}

	// Setup handlers
	a.setupHandlers()

	// Authenticate
	done := make(chan int, 1)
	var authError string

	a.client.OnPacket(protocol.TypeOk, func(parts []string) {
		if len(parts) >= 2 && parts[1] == protocol.TypeAuth {
			select {
			case done <- 1:
			default:
			}
		}
	})

	a.client.OnPacket(protocol.TypeFail, func(parts []string) {
		if len(parts) >= 2 && parts[1] == protocol.TypeAuth {
			if len(parts) >= 3 {
				authError = parts[2]
			} else {
				authError = "Auth failed"
			}
			select {
			case done <- -1:
			default:
			}
		}
	})

	a.client.Auth(a.currentUser, a.currentPass)

	select {
	case result := <-done:
		a.app.QueueUpdateDraw(func() {
			if result == 1 {
				a.updateConnectionStatus()
				a.updateStatusBarText()
				a.loadContacts()
				a.loadStatuses()
				a.loadOfflineMessages()
			} else {
				a.setConnectionError(authError)
				a.client.Disconnect()
				a.client = nil
				a.updateStatusBarText()
			}
		})
	case <-time.After(10 * time.Second):
		a.app.QueueUpdateDraw(func() {
			a.setConnectionError("Connection timeout")
			if a.client != nil {
				a.client.Disconnect()
				a.client = nil
			}
			a.updateStatusBarText()
		})
	}
}
