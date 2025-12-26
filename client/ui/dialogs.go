package ui

import (
	"fmt"
	"time"

	"msim-client/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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
