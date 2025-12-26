package ui

import (
	"fmt"
	"time"

	"msim-client/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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
