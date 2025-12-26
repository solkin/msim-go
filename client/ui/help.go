package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

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

