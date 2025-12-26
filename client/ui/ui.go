package ui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"msim-client/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Colors - Midnight Commander style
var (
	ColorBg        = tcell.NewRGBColor(0, 0, 128)     // Dark blue background
	ColorFg        = tcell.NewRGBColor(192, 192, 192) // Light gray text
	ColorBorder    = tcell.NewRGBColor(0, 255, 255)   // Cyan borders
	ColorTitle     = tcell.NewRGBColor(255, 255, 255) // White titles
	ColorHighlight = tcell.NewRGBColor(0, 255, 255)   // Cyan highlight
	ColorOnline    = tcell.NewRGBColor(0, 255, 0)     // Green for online
	ColorOffline   = tcell.NewRGBColor(128, 128, 128) // Gray for offline
	ColorSent      = tcell.NewRGBColor(255, 255, 0)   // Yellow for sent
	ColorReceived  = tcell.NewRGBColor(0, 255, 255)   // Cyan for received
	ColorAck       = tcell.NewRGBColor(0, 255, 0)     // Green for acknowledged
)

// App is the main application
type App struct {
	app                *tview.Application
	pages              *tview.Pages
	client             *protocol.Client
	serverAddr         string
	currentUser        string
	currentPass        string
	contacts           []protocol.Contact
	statuses           map[string]bool
	statusLastSeen     map[string]string // last seen timestamp per contact
	unreadCounts       map[string]int    // unread message count per contact
	unreadMarker       int               // position of unread marker in current chat (messages before this are read)
	pendingUnreadCount int               // temporary storage for unread count before history loads
	messages           map[string][]protocol.Message
	currentChat        string
	mu                 sync.RWMutex
	contactsList       *tview.List
	chatView           *tview.TextView
	messageInput       *tview.InputField
	statusBar          *tview.TextView
	connectionView     *tview.TextView
	statusTicker       *time.Ticker
	statusTickerDone   chan struct{}
}

// NewApp creates a new application instance
func NewApp(serverAddr string) *App {
	return &App{
		serverAddr:     serverAddr,
		statuses:       make(map[string]bool),
		statusLastSeen: make(map[string]string),
		unreadCounts:   make(map[string]int),
		messages:       make(map[string][]protocol.Message),
	}
}

// Run starts the application
func (a *App) Run() error {
	a.app = tview.NewApplication()
	a.pages = tview.NewPages()

	// Create empty background
	background := tview.NewBox()
	background.SetBackgroundColor(tcell.NewRGBColor(64, 64, 64))
	a.pages.AddPage("background", background, true, true)

	// Show auth dialog on top
	a.showAuthDialog()

	return a.app.SetRoot(a.pages, true).EnableMouse(false).Run()
}

func (a *App) showAuthDialog() {
	// Form container
	form := tview.NewForm()
	form.SetBackgroundColor(ColorBg)
	form.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	form.SetFieldTextColor(ColorFg)
	form.SetLabelColor(ColorHighlight)
	form.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	form.SetButtonTextColor(ColorTitle)
	form.SetBorder(true)
	form.SetBorderColor(ColorBorder)
	form.SetTitle(" mSIM Authorization ")
	form.SetTitleColor(ColorTitle)

	var loginField, passwordField *tview.InputField
	var statusText *tview.TextView

	statusText = tview.NewTextView()
	statusText.SetBackgroundColor(ColorBg)
	statusText.SetTextColor(tcell.ColorRed)
	statusText.SetTextAlign(tview.AlignCenter)
	statusText.SetDynamicColors(true)

	loginField = tview.NewInputField()
	loginField.SetLabel("Login: ")
	loginField.SetFieldWidth(30)
	loginField.SetBackgroundColor(ColorBg)

	passwordField = tview.NewInputField()
	passwordField.SetLabel("Password: ")
	passwordField.SetFieldWidth(30)
	passwordField.SetMaskCharacter('*')
	passwordField.SetBackgroundColor(ColorBg)

	form.AddFormItem(loginField)
	form.AddFormItem(passwordField)

	// Auth button
	form.AddButton("Login", func() {
		login := loginField.GetText()
		password := passwordField.GetText()
		if login == "" || password == "" {
			statusText.SetText("[red]Please enter login and password[-]")
			return
		}
		a.doAuth(login, password, statusText, false)
	})

	// Register button
	form.AddButton("Register", func() {
		login := loginField.GetText()
		password := passwordField.GetText()
		if login == "" || password == "" {
			statusText.SetText("[red]Please enter login and password[-]")
			return
		}
		a.doAuth(login, password, statusText, true)
	})

	form.AddButton("Quit", func() {
		a.app.Stop()
	})

	// Center the form
	formFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(statusText, 1, 0, false)

	// Create modal-like container
	width := 54
	height := 12

	modal := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(formFlex, width, 0, true).
			AddItem(nil, 0, 1, false), height, 0, true).
		AddItem(nil, 0, 1, false)

	a.pages.AddPage("auth", modal, true, true)
	a.app.SetFocus(form)
}

func (a *App) doAuth(login, password string, statusText *tview.TextView, register bool) {
	statusText.SetText("Connecting...")

	// Run connection in goroutine to avoid blocking UI
	go func() {
		// Connect to server
		a.client = protocol.NewClient()
		err := a.client.Connect(a.serverAddr)
		if err != nil {
			a.app.QueueUpdateDraw(func() {
				statusText.SetText(fmt.Sprintf("Connection failed: %v", err))
			})
			return
		}

		// Setup handlers
		a.setupHandlers()

		// Channel for results: 1 = auth success, 0 = reg success (need auth), -1 = error
		done := make(chan int, 1)
		var authError string

		// Handle responses
		a.client.OnPacket(protocol.TypeOk, func(parts []string) {
			if len(parts) >= 2 {
				op := parts[1]
				if op == protocol.TypeAuth {
					a.currentUser = login
					a.currentPass = password
					select {
					case done <- 1: // auth success
					default:
					}
				} else if op == protocol.TypeReg {
					select {
					case done <- 0: // reg success, need to auth
					default:
					}
				}
			}
		})

		a.client.OnPacket(protocol.TypeFail, func(parts []string) {
			if len(parts) >= 2 {
				op := parts[1]
				if op == protocol.TypeAuth || op == protocol.TypeReg {
					if len(parts) >= 3 {
						authError = parts[2]
					} else {
						authError = "Operation failed"
					}
					select {
					case done <- -1: // error
					default:
					}
				}
			}
		})

		// Send auth or register
		a.app.QueueUpdateDraw(func() {
			if register {
				statusText.SetText("Registering...")
			} else {
				statusText.SetText("Authenticating...")
			}
		})

		if register {
			a.client.Register(login, password)
		} else {
			a.client.Auth(login, password)
		}

		// Wait for response
		select {
		case result := <-done:
			if result == 1 {
				// Auth success
				a.app.QueueUpdateDraw(func() {
					a.showMainScreen()
				})
			} else if result == 0 {
				// Registration success, now authenticate
				a.app.QueueUpdateDraw(func() {
					statusText.SetText("Registered! Authenticating...")
				})
				a.client.Auth(login, password)

				// Wait for auth response
				select {
				case authResult := <-done:
					a.app.QueueUpdateDraw(func() {
						if authResult == 1 {
							a.showMainScreen()
						} else {
							statusText.SetText(authError)
							a.client.Disconnect()
						}
					})
				case <-time.After(10 * time.Second):
					a.app.QueueUpdateDraw(func() {
						statusText.SetText("Auth timeout after registration")
						a.client.Disconnect()
					})
				}
			} else {
				// Error
				a.app.QueueUpdateDraw(func() {
					statusText.SetText(authError)
					a.client.Disconnect()
				})
			}
		case <-time.After(10 * time.Second):
			a.app.QueueUpdateDraw(func() {
				statusText.SetText("Connection timeout")
				a.client.Disconnect()
			})
		}
	}()
}

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

func (a *App) showMainScreen() {
	// Remove auth dialog and background
	a.pages.RemovePage("auth")
	a.pages.RemovePage("background")

	// Create and add main page
	mainPage := a.createMainPage()
	a.pages.AddPage("main", mainPage, true, true)

	// Update title with current user
	a.contactsList.SetTitle(fmt.Sprintf(" Contacts [%s] ", a.currentUser))

	// Start status ticker for ping display
	a.startStatusTicker()

	// Update connection status
	a.updateConnectionStatus()
	a.updateStatusBarText()

	// Load contacts, statuses and offline messages
	a.loadContacts()
	a.loadStatuses()
	a.loadOfflineMessages()

	// Focus on contacts list
	a.app.SetFocus(a.contactsList)
}

func (a *App) createMainPage() tview.Primitive {
	// Contacts list on the left
	a.contactsList = tview.NewList()
	a.contactsList.SetBorder(true)
	a.contactsList.SetBorderColor(ColorBorder)
	a.contactsList.SetBackgroundColor(ColorBg)
	a.contactsList.SetTitle(" Contacts ")
	a.contactsList.SetTitleColor(ColorTitle)
	a.contactsList.SetMainTextColor(ColorFg)
	a.contactsList.SetMainTextStyle(tcell.StyleDefault.Foreground(ColorFg).Background(ColorBg))
	a.contactsList.SetSelectedTextColor(ColorTitle)
	a.contactsList.SetSelectedBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	a.contactsList.SetHighlightFullLine(true)
	a.contactsList.ShowSecondaryText(false)

	a.contactsList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		// Check connection first
		if a.client == nil || !a.client.IsConnected() {
			a.setConnectionError("Not connected. Press F6 to connect.")
			return
		}
		a.mu.RLock()
		if index < len(a.contacts) {
			contact := a.contacts[index]
			a.mu.RUnlock()
			a.openChat(contact.ID)
		} else {
			a.mu.RUnlock()
		}
	})

	// Connection status view
	a.connectionView = tview.NewTextView()
	a.connectionView.SetBorder(true)
	a.connectionView.SetBorderColor(ColorBorder)
	a.connectionView.SetBackgroundColor(ColorBg)
	a.connectionView.SetTitle(" Connection ")
	a.connectionView.SetTitleColor(ColorTitle)
	a.connectionView.SetTextColor(ColorFg)
	a.connectionView.SetDynamicColors(true)
	a.connectionView.SetTextAlign(tview.AlignCenter)
	a.updateConnectionStatus()

	// Status bar at bottom
	a.statusBar = tview.NewTextView()
	a.statusBar.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	a.statusBar.SetTextColor(ColorTitle)
	a.statusBar.SetTextAlign(tview.AlignCenter)
	a.updateStatusBarText()

	// Main layout
	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.contactsList, 0, 1, true).
		AddItem(a.connectionView, 3, 0, false).
		AddItem(a.statusBar, 1, 0, false)
	mainFlex.SetBackgroundColor(ColorBg)

	// Handle keyboard
	mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyF1:
			a.showHelp()
			return nil
		case tcell.KeyF2:
			a.showAddContactDialog()
			return nil
		case tcell.KeyF3:
			a.showRenameContactDialog()
			return nil
		case tcell.KeyF4:
			a.showDeleteContactDialog()
			return nil
		case tcell.KeyF5:
			a.loadContacts()
			a.loadStatuses()
			return nil
		case tcell.KeyF6:
			a.toggleConnection()
			return nil
		case tcell.KeyF10:
			a.quit()
			return nil
		case tcell.KeyEsc:
			a.quit()
			return nil
		}
		return event
	})

	return mainFlex
}

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

// formatLastSeen formats the last seen timestamp for display
func (a *App) formatLastSeen(timestamp string) string {
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
				if formatted := a.formatLastSeen(ts); formatted != "" {
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

	for i, msg := range messages {
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

func (a *App) showAddContactDialog() {
	form := tview.NewForm()
	form.SetBackgroundColor(ColorBg)
	form.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	form.SetFieldTextColor(ColorFg)
	form.SetLabelColor(ColorHighlight)
	form.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	form.SetButtonTextColor(ColorTitle)
	form.SetBorder(true)
	form.SetBorderColor(ColorBorder)
	form.SetTitle(" Add Contact ")
	form.SetTitleColor(ColorTitle)

	var idField, nickField *tview.InputField
	var statusLabel *tview.TextView

	statusLabel = tview.NewTextView()
	statusLabel.SetBackgroundColor(ColorBg)
	statusLabel.SetTextColor(tcell.ColorRed)

	idField = tview.NewInputField()
	idField.SetLabel("User ID: ")
	idField.SetFieldWidth(30)

	nickField = tview.NewInputField()
	nickField.SetLabel("Nickname: ")
	nickField.SetFieldWidth(30)

	form.AddFormItem(idField)
	form.AddFormItem(nickField)

	form.AddButton("Add", func() {
		id := idField.GetText()
		nick := nickField.GetText()
		if id == "" {
			statusLabel.SetText("User ID is required")
			return
		}

		done := make(chan bool, 1)
		var errMsg string

		a.client.OnPacket(protocol.TypeOk, func(parts []string) {
			if len(parts) >= 2 && parts[1] == protocol.TypeAdd {
				done <- true
			}
		})

		a.client.OnPacket(protocol.TypeFail, func(parts []string) {
			if len(parts) >= 2 && parts[1] == protocol.TypeAdd {
				if len(parts) >= 3 {
					errMsg = parts[2]
				} else {
					errMsg = "Failed to add contact"
				}
				done <- false
			}
		})

		if nick != "" {
			a.client.AddContact(id, nick)
		} else {
			a.client.AddContact(id)
		}

		go func() {
			select {
			case success := <-done:
				a.app.QueueUpdateDraw(func() {
					if success {
						a.pages.RemovePage("dialog")
						a.loadContacts()
						a.loadStatuses()
					} else {
						statusLabel.SetText(errMsg)
					}
				})
			case <-time.After(5 * time.Second):
				a.app.QueueUpdateDraw(func() {
					statusLabel.SetText("Timeout")
				})
			}
		}()
	})

	form.AddButton("Cancel", func() {
		a.pages.RemovePage("dialog")
		a.app.SetFocus(a.contactsList)
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(form, 50, 0, true).
			AddItem(nil, 0, 1, false), 10, 0, true).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(statusLabel, 50, 0, false).
			AddItem(nil, 0, 1, false), 1, 0, false).
		AddItem(nil, 0, 1, false)
	flex.SetBackgroundColor(ColorBg)

	a.pages.AddPage("dialog", flex, true, true)
	a.app.SetFocus(form)
}

func (a *App) showRenameContactDialog() {
	idx := a.contactsList.GetCurrentItem()
	a.mu.RLock()
	if idx < 0 || idx >= len(a.contacts) {
		a.mu.RUnlock()
		return
	}
	contact := a.contacts[idx]
	a.mu.RUnlock()

	form := tview.NewForm()
	form.SetBackgroundColor(ColorBg)
	form.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	form.SetFieldTextColor(ColorFg)
	form.SetLabelColor(ColorHighlight)
	form.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	form.SetButtonTextColor(ColorTitle)
	form.SetBorder(true)
	form.SetBorderColor(ColorBorder)
	form.SetTitle(fmt.Sprintf(" Rename %s ", contact.ID))
	form.SetTitleColor(ColorTitle)

	var nickField *tview.InputField
	var statusLabel *tview.TextView

	statusLabel = tview.NewTextView()
	statusLabel.SetBackgroundColor(ColorBg)
	statusLabel.SetTextColor(tcell.ColorRed)

	nickField = tview.NewInputField()
	nickField.SetLabel("New nickname: ")
	nickField.SetFieldWidth(30)
	nickField.SetText(contact.Nick)

	form.AddFormItem(nickField)

	form.AddButton("Rename", func() {
		nick := nickField.GetText()
		if nick == "" {
			statusLabel.SetText("Nickname is required")
			return
		}

		done := make(chan bool, 1)
		var errMsg string

		a.client.OnPacket(protocol.TypeOk, func(parts []string) {
			if len(parts) >= 2 && parts[1] == protocol.TypeRen {
				done <- true
			}
		})

		a.client.OnPacket(protocol.TypeFail, func(parts []string) {
			if len(parts) >= 2 && parts[1] == protocol.TypeRen {
				if len(parts) >= 3 {
					errMsg = parts[2]
				} else {
					errMsg = "Failed to rename contact"
				}
				done <- false
			}
		})

		a.client.RenameContact(contact.ID, nick)

		go func() {
			select {
			case success := <-done:
				a.app.QueueUpdateDraw(func() {
					if success {
						a.pages.RemovePage("dialog")
						a.loadContacts()
					} else {
						statusLabel.SetText(errMsg)
					}
				})
			case <-time.After(5 * time.Second):
				a.app.QueueUpdateDraw(func() {
					statusLabel.SetText("Timeout")
				})
			}
		}()
	})

	form.AddButton("Cancel", func() {
		a.pages.RemovePage("dialog")
		a.app.SetFocus(a.contactsList)
	})

	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(form, 50, 0, true).
			AddItem(nil, 0, 1, false), 8, 0, true).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(statusLabel, 50, 0, false).
			AddItem(nil, 0, 1, false), 1, 0, false).
		AddItem(nil, 0, 1, false)
	flex.SetBackgroundColor(ColorBg)

	a.pages.AddPage("dialog", flex, true, true)
	a.app.SetFocus(form)
}

func (a *App) showDeleteContactDialog() {
	idx := a.contactsList.GetCurrentItem()
	a.mu.RLock()
	if idx < 0 || idx >= len(a.contacts) {
		a.mu.RUnlock()
		return
	}
	contact := a.contacts[idx]
	a.mu.RUnlock()

	modal := tview.NewModal()
	modal.SetText(fmt.Sprintf("Delete contact %s (%s)?", contact.Nick, contact.ID))
	modal.SetBackgroundColor(ColorBg)
	modal.SetTextColor(ColorFg)
	modal.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	modal.SetButtonTextColor(ColorTitle)
	modal.AddButtons([]string{"Delete", "Cancel"})
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		if buttonLabel == "Delete" {
			done := make(chan bool, 1)

			a.client.OnPacket(protocol.TypeOk, func(parts []string) {
				if len(parts) >= 2 && parts[1] == protocol.TypeDel {
					done <- true
				}
			})

			a.client.DeleteContact(contact.ID)

			go func() {
				select {
				case <-done:
					a.app.QueueUpdateDraw(func() {
						a.pages.RemovePage("dialog")
						a.loadContacts()
					})
				case <-time.After(5 * time.Second):
					a.app.QueueUpdateDraw(func() {
						a.pages.RemovePage("dialog")
					})
				}
			}()
		} else {
			a.pages.RemovePage("dialog")
			a.app.SetFocus(a.contactsList)
		}
	})

	a.pages.AddPage("dialog", modal, true, true)
}

func (a *App) showClearHistoryDialog(contactID string) {
	modal := tview.NewModal()
	modal.SetText(fmt.Sprintf("Clear history with %s?", contactID))
	modal.SetBackgroundColor(ColorBg)
	modal.SetTextColor(ColorFg)
	modal.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	modal.SetButtonTextColor(ColorTitle)
	modal.AddButtons([]string{"Clear", "Cancel"})
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		if buttonLabel == "Clear" {
			a.client.ClearHistory(contactID)
			a.mu.Lock()
			a.messages[contactID] = nil
			a.mu.Unlock()
			a.refreshChatView()
		}
		a.pages.RemovePage("dialog")
		a.app.SetFocus(a.messageInput)
	})

	a.pages.AddPage("dialog", modal, true, true)
}

func (a *App) showHelp() {
	helpText := `
 [yellow]Main Screen[-]
 ───────────────────────────────────────────────────────────────
   [white]F1[-]       Show this help
   [white]F2[-]       Add new contact
   [white]F3[-]       Rename selected contact
   [white]F4[-]       Delete selected contact
   [white]F5[-]       Refresh contacts list
   [white]F6[-]       Connect / Disconnect
   [white]F10/Esc[-]  Quit application
   [white]Enter[-]    Open chat with contact
   [white]↑ ↓[-]      Navigate contacts

 [yellow]Chat Screen[-]
 ───────────────────────────────────────────────────────────────
   [white]Enter[-]    Send message
   [white]Tab[-]      Switch between input and scroll mode
   [white]Esc[-]      Back to contacts (from input mode)

 [yellow]Scroll Mode (after pressing Tab)[-]
 ───────────────────────────────────────────────────────────────
   [white]↑ ↓[-]      Scroll one line
   [white]PgUp/Dn[-]  Scroll page (10 lines)
   [white]Home[-]     Scroll to beginning
   [white]End[-]      Scroll to end
   [white]Tab/Esc[-]  Return to input mode

 [yellow]Other Chat Keys[-]
 ───────────────────────────────────────────────────────────────
   [white]F5[-]       Refresh history
   [white]F8[-]       Clear history

 [yellow]Status Icons[-]
 ───────────────────────────────────────────────────────────────
   [green]●[-] online   User is connected
   [gray]○[-] offline  User is disconnected
   [green]✓[-]          Message delivered (acknowledged)
   [gray]○[-]          Message sent (waiting for ack)

 [yellow]Protocol Information[-]
 ───────────────────────────────────────────────────────────────
   Server connection is kept alive with automatic ping every 30s.
   Incoming messages are automatically acknowledged (ack).
   Messages from unknown users auto-add them to contacts.
`

	helpView := tview.NewTextView()
	helpView.SetText(helpText)
	helpView.SetBackgroundColor(ColorBg)
	helpView.SetTextColor(ColorFg)
	helpView.SetDynamicColors(true)
	helpView.SetBorder(true)
	helpView.SetBorderColor(ColorBorder)
	helpView.SetTitle(" Help ")
	helpView.SetTitleColor(ColorTitle)
	helpView.SetScrollable(true)

	// Status bar
	statusBar := tview.NewTextView()
	statusBar.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	statusBar.SetTextColor(ColorTitle)
	statusBar.SetTextAlign(tview.AlignCenter)
	statusBar.SetText(" ↑↓/PgUp/PgDn: Scroll | Esc/Enter/F1: Close ")

	// Layout
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(helpView, 0, 1, true).
		AddItem(statusBar, 1, 0, false)
	flex.SetBackgroundColor(ColorBg)

	// Handle keyboard
	flex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc, tcell.KeyEnter, tcell.KeyF1:
			a.pages.RemovePage("help")
			a.app.SetFocus(a.contactsList)
			return nil
		case tcell.KeyUp:
			row, col := helpView.GetScrollOffset()
			helpView.ScrollTo(row-1, col)
			return nil
		case tcell.KeyDown:
			row, col := helpView.GetScrollOffset()
			helpView.ScrollTo(row+1, col)
			return nil
		case tcell.KeyPgUp:
			row, col := helpView.GetScrollOffset()
			helpView.ScrollTo(row-10, col)
			return nil
		case tcell.KeyPgDn:
			row, col := helpView.GetScrollOffset()
			helpView.ScrollTo(row+10, col)
			return nil
		case tcell.KeyHome:
			helpView.ScrollToBeginning()
			return nil
		case tcell.KeyEnd:
			helpView.ScrollToEnd()
			return nil
		}
		return event
	})

	a.pages.AddPage("help", flex, true, true)
}

func (a *App) showDisconnectNotification(reason, details string) {
	reasonText := "Disconnected"
	switch reason {
	case "timeout":
		reasonText = "Session timeout - no activity"
	case "maintenance":
		if details != "" {
			reasonText = fmt.Sprintf("Server maintenance until %s", details)
		} else {
			reasonText = "Server is going to maintenance"
		}
	case "restart":
		if details != "" {
			reasonText = fmt.Sprintf("Server restarting, back at %s", details)
		} else {
			reasonText = "Server is restarting"
		}
	case "connection_lost":
		reasonText = "Connection lost"
	}

	if a.connectionView != nil {
		a.connectionView.SetText(fmt.Sprintf("[red]○ %s[-]\n[gray]Press F6 to reconnect[-]", reasonText))
	}
}

func (a *App) showDisconnectDialog(reason string) {
	reasonText := "Disconnected from server"
	switch reason {
	case "timeout":
		reasonText = "Session timeout - no activity"
	case "maintenance":
		reasonText = "Server is going to maintenance"
	case "restart":
		reasonText = "Server is restarting"
	case "connection_lost":
		reasonText = "Connection lost"
	}

	modal := tview.NewModal()
	modal.SetText(reasonText)
	modal.SetBackgroundColor(ColorBg)
	modal.SetTextColor(ColorFg)
	modal.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	modal.SetButtonTextColor(ColorTitle)
	modal.AddButtons([]string{"OK"})
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		a.app.Stop()
	})

	a.pages.AddPage("disconnect", modal, true, true)
}

func (a *App) quit() {
	if a.client != nil && a.client.IsConnected() {
		a.client.Disconnect()
	}
	a.app.Stop()
}
