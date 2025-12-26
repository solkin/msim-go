package ui

import (
	"sync"
	"time"

	"msim-client/protocol"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

// quit exits the application
func (a *App) quit() {
	if a.client != nil && a.client.IsConnected() {
		a.client.Disconnect()
	}
	a.app.Stop()
}
