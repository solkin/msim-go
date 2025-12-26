package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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

