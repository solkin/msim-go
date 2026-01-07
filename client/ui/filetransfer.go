package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// FileTransfer represents an active file transfer
type FileTransfer struct {
	SessionID   string
	Direction   string // "send" | "receive"
	Filename    string
	Size        int64
	Hash        string
	Contact     string
	SavePath    string // for receiving
	FilePath    string // for sending
	Port        int
	BytesDone   int64
	StartTime   time.Time
	Status      string // "pending" | "waiting" | "transferring" | "completed" | "failed" | "cancelled"
	Error       string
	mu          sync.Mutex
}

// activeTransfer holds the current transfer (only one at a time)
var activeTransfer *FileTransfer
var transferMu sync.Mutex

// isTransferActive checks if there's an active transfer
func isTransferActive() bool {
	transferMu.Lock()
	defer transferMu.Unlock()
	return activeTransfer != nil && (activeTransfer.Status == "pending" || 
		activeTransfer.Status == "waiting" || activeTransfer.Status == "transferring")
}

// showSendFileDialog shows the dialog for sending a file
func (a *App) showSendFileDialog(recipient string) {
	if isTransferActive() {
		a.showErrorDialog("Transfer in progress", "Please wait for the current transfer to complete.")
		return
	}

	var pathInput *tview.InputField
	var sizeLabel *tview.TextView
	var statusLabel *tview.TextView

	form := tview.NewForm()
	form.SetBackgroundColor(ColorBg)
	form.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	form.SetFieldTextColor(ColorFg)
	form.SetLabelColor(ColorHighlight)
	form.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	form.SetButtonTextColor(ColorTitle)
	form.SetBorder(true)
	form.SetBorderColor(ColorBorder)
	form.SetTitle(fmt.Sprintf(" Send File to %s ", recipient))
	form.SetTitleColor(ColorTitle)

	// File path input
	pathInput = tview.NewInputField()
	pathInput.SetLabel("File: ")
	pathInput.SetFieldWidth(40)
	pathInput.SetChangedFunc(func(text string) {
		if info, err := os.Stat(text); err == nil && !info.IsDir() {
			sizeLabel.SetText(fmt.Sprintf("Size: %s", formatFileSize(info.Size())))
		} else {
			sizeLabel.SetText("Size: -")
		}
	})

	// Size label
	sizeLabel = tview.NewTextView()
	sizeLabel.SetBackgroundColor(ColorBg)
	sizeLabel.SetTextColor(ColorFg)
	sizeLabel.SetText("Size: -")

	// Status label
	statusLabel = tview.NewTextView()
	statusLabel.SetBackgroundColor(ColorBg)
	statusLabel.SetTextColor(tcell.ColorRed)
	statusLabel.SetDynamicColors(true)

	form.AddFormItem(pathInput)

	form.AddButton("Browse", func() {
		a.showFileBrowser(FileBrowserModeFile, pathInput.GetText(), "", func(result FileBrowserResult) {
			if result.Selected {
				pathInput.SetText(result.Path)
				if info, err := os.Stat(result.Path); err == nil {
					sizeLabel.SetText(fmt.Sprintf("Size: %s", formatFileSize(info.Size())))
				}
			}
			a.app.SetFocus(form)
		})
	})

	form.AddButton("Send", func() {
		filePath := pathInput.GetText()
		if filePath == "" {
			statusLabel.SetText("[red]Please select a file")
			return
		}

		info, err := os.Stat(filePath)
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("[red]File not found: %v", err))
			return
		}
		if info.IsDir() {
			statusLabel.SetText("[red]Cannot send a directory")
			return
		}

		// Calculate hash
		statusLabel.SetText("[yellow]Calculating hash...")
		a.app.ForceDraw()

		hash, err := calculateFileHash(filePath)
		if err != nil {
			statusLabel.SetText(fmt.Sprintf("[red]Hash error: %v", err))
			return
		}

		// Create transfer
		transfer := &FileTransfer{
			Direction: "send",
			Filename:  filepath.Base(filePath),
			Size:      info.Size(),
			Hash:      hash,
			Contact:   recipient,
			FilePath:  filePath,
			Status:    "pending",
		}

		transferMu.Lock()
		activeTransfer = transfer
		transferMu.Unlock()

		// Close this dialog and show waiting dialog
		a.pages.RemovePage("sendfile")
		a.showSendWaitingDialog(transfer)

		// Send file request to server
		a.client.SendFile(recipient, transfer.Filename, transfer.Size, transfer.Hash)
	})

	form.AddButton("Cancel", func() {
		a.pages.RemovePage("sendfile")
		a.app.SetFocus(a.messageInput)
	})

	// Layout
	flex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(form, 9, 0, true).
				AddItem(sizeLabel, 1, 0, false).
				AddItem(statusLabel, 1, 0, false), 55, 0, true).
			AddItem(nil, 0, 1, false), 13, 0, true).
		AddItem(nil, 0, 1, false)
	flex.SetBackgroundColor(ColorBg)

	a.pages.AddPage("sendfile", flex, true, true)
	a.app.SetFocus(form)
}

// showSendWaitingDialog shows dialog while waiting for recipient to accept
func (a *App) showSendWaitingDialog(transfer *FileTransfer) {
	transfer.Status = "waiting"

	contentView := tview.NewTextView()
	contentView.SetBackgroundColor(ColorBg)
	contentView.SetTextColor(ColorFg)
	contentView.SetDynamicColors(true)
	contentView.SetTextAlign(tview.AlignCenter)
	contentView.SetBorder(true)
	contentView.SetBorderColor(ColorBorder)
	contentView.SetTitle(" Send File ")
	contentView.SetTitleColor(ColorTitle)

	updateContent := func(expiresIn int) {
		content := fmt.Sprintf("\n📤 %s → %s\nSize: %s\n\nWaiting for recipient...\n⏱ Expires in %d:%02d\n",
			transfer.Filename, transfer.Contact,
			formatFileSize(transfer.Size),
			expiresIn/60, expiresIn%60)
		contentView.SetText(content)
	}

	// Initial expiry (5 minutes)
	expiresIn := 300
	updateContent(expiresIn)

	// Create cancel button
	cancelBtn := tview.NewButton("Cancel")
	cancelBtn.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	cancelBtn.SetLabelColor(ColorTitle)
	cancelBtn.SetSelectedFunc(func() {
		transfer.Status = "cancelled"
		if transfer.SessionID != "" {
			a.client.CancelFile(transfer.Contact, transfer.SessionID, "user cancelled")
		}
		transferMu.Lock()
		activeTransfer = nil
		transferMu.Unlock()
		a.pages.RemovePage("sendwaiting")
		a.app.SetFocus(a.messageInput)
	})

	// Layout
	buttonFlex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(cancelBtn, 10, 0, true).
		AddItem(nil, 0, 1, false)

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(contentView, 9, 0, false).
				AddItem(buttonFlex, 1, 0, true), 45, 0, true).
			AddItem(nil, 0, 1, false), 12, 0, true).
		AddItem(nil, 0, 1, false)
	mainFlex.SetBackgroundColor(ColorBg)

	// Handle escape
	mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			transfer.Status = "cancelled"
			if transfer.SessionID != "" {
				a.client.CancelFile(transfer.Contact, transfer.SessionID, "user cancelled")
			}
			transferMu.Lock()
			activeTransfer = nil
			transferMu.Unlock()
			a.pages.RemovePage("sendwaiting")
			a.app.SetFocus(a.messageInput)
			return nil
		}
		return event
	})

	a.pages.AddPage("sendwaiting", mainFlex, true, true)
	a.app.SetFocus(cancelBtn)

	// Start countdown timer
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			transfer.mu.Lock()
			status := transfer.Status
			transfer.mu.Unlock()

			if status != "waiting" {
				return
			}

			expiresIn--
			if expiresIn <= 0 {
				transfer.Status = "failed"
				transfer.Error = "timeout"
				transferMu.Lock()
				activeTransfer = nil
				transferMu.Unlock()
				a.app.QueueUpdateDraw(func() {
					a.pages.RemovePage("sendwaiting")
					a.showErrorDialog("Transfer Failed", "Recipient did not respond in time.")
				})
				return
			}

			a.app.QueueUpdateDraw(func() {
				updateContent(expiresIn)
			})
		}
	}()
}

// showSendProgressDialog shows the progress dialog for sending
func (a *App) showSendProgressDialog(transfer *FileTransfer) {
	transfer.Status = "transferring"
	transfer.StartTime = time.Now()

	contentView := tview.NewTextView()
	contentView.SetBackgroundColor(ColorBg)
	contentView.SetTextColor(ColorFg)
	contentView.SetDynamicColors(true)
	contentView.SetTextAlign(tview.AlignCenter)
	contentView.SetBorder(true)
	contentView.SetBorderColor(ColorBorder)
	contentView.SetTitle(" Send File ")
	contentView.SetTitleColor(ColorTitle)

	updateProgress := func() {
		transfer.mu.Lock()
		bytesDone := transfer.BytesDone
		status := transfer.Status
		transfer.mu.Unlock()

		if status != "transferring" {
			return
		}

		percent := 0
		if transfer.Size > 0 {
			percent = int(bytesDone * 100 / transfer.Size)
		}

		elapsed := time.Since(transfer.StartTime).Seconds()
		speed := float64(0)
		if elapsed > 0 {
			speed = float64(bytesDone) / elapsed
		}

		progressBar := buildProgressBar(percent, 30)

		content := fmt.Sprintf("\n📤 %s → %s\n\n%s %d%%\n%s / %s  •  %s/s\n",
			transfer.Filename, transfer.Contact,
			progressBar, percent,
			formatFileSize(bytesDone), formatFileSize(transfer.Size),
			formatFileSize(int64(speed)))

		contentView.SetText(content)
	}

	updateProgress()

	// Cancel button
	cancelBtn := tview.NewButton("Cancel")
	cancelBtn.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	cancelBtn.SetLabelColor(ColorTitle)
	cancelBtn.SetSelectedFunc(func() {
		transfer.mu.Lock()
		transfer.Status = "cancelled"
		transfer.mu.Unlock()
		a.client.CancelFile(transfer.Contact, transfer.SessionID, "user cancelled")
		transferMu.Lock()
		activeTransfer = nil
		transferMu.Unlock()
		a.pages.RemovePage("sendprogress")
		a.app.SetFocus(a.messageInput)
	})

	buttonFlex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(cancelBtn, 10, 0, true).
		AddItem(nil, 0, 1, false)

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(contentView, 8, 0, false).
				AddItem(buttonFlex, 1, 0, true), 50, 0, true).
			AddItem(nil, 0, 1, false), 11, 0, true).
		AddItem(nil, 0, 1, false)
	mainFlex.SetBackgroundColor(ColorBg)

	mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			transfer.mu.Lock()
			transfer.Status = "cancelled"
			transfer.mu.Unlock()
			a.client.CancelFile(transfer.Contact, transfer.SessionID, "user cancelled")
			transferMu.Lock()
			activeTransfer = nil
			transferMu.Unlock()
			a.pages.RemovePage("sendprogress")
			a.app.SetFocus(a.messageInput)
			return nil
		}
		return event
	})

	a.pages.AddPage("sendprogress", mainFlex, true, true)
	a.app.SetFocus(cancelBtn)

	// Start the actual file transfer in background
	go a.performSendTransfer(transfer, func() {
		a.app.QueueUpdateDraw(updateProgress)
	})

	// Update progress periodically
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			transfer.mu.Lock()
			status := transfer.Status
			transfer.mu.Unlock()

			if status != "transferring" {
				return
			}

			a.app.QueueUpdateDraw(updateProgress)
		}
	}()
}

// performSendTransfer performs the actual file transfer
func (a *App) performSendTransfer(transfer *FileTransfer, onProgress func()) {
	// Open file
	file, err := os.Open(transfer.FilePath)
	if err != nil {
		transfer.mu.Lock()
		transfer.Status = "failed"
		transfer.Error = err.Error()
		transfer.mu.Unlock()
		a.showTransferResult(transfer)
		return
	}
	defer file.Close()

	// Connect to upload port
	serverAddr := a.client.GetServerAddr()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", serverAddr, transfer.Port), 30*time.Second)
	if err != nil {
		transfer.mu.Lock()
		transfer.Status = "failed"
		transfer.Error = err.Error()
		transfer.mu.Unlock()
		a.showTransferResult(transfer)
		return
	}
	defer conn.Close()

	// Create progress reader
	reader := &progressReader{
		reader: file,
		onProgress: func(n int64) {
			transfer.mu.Lock()
			transfer.BytesDone = n
			transfer.mu.Unlock()
			onProgress()
		},
	}

	// Copy file to connection
	_, err = io.Copy(conn, reader)

	transfer.mu.Lock()
	if err != nil && transfer.Status == "transferring" {
		transfer.Status = "failed"
		transfer.Error = err.Error()
	} else if transfer.Status == "transferring" {
		transfer.Status = "completed"
	}
	transfer.mu.Unlock()

	a.showTransferResult(transfer)
}

// showReceiveFileDialog shows the dialog for incoming file
func (a *App) showReceiveFileDialog(sender, filename string, size int64, hash, sessionID string) {
	if isTransferActive() {
		// Auto-decline if busy
		a.client.DeclineFile(sender, sessionID, "busy")
		return
	}

	// Default save path
	home, _ := os.UserHomeDir()
	defaultPath := filepath.Join(home, "Downloads", filename)

	// Create transfer
	transfer := &FileTransfer{
		SessionID: sessionID,
		Direction: "receive",
		Filename:  filename,
		Size:      size,
		Hash:      hash,
		Contact:   sender,
		SavePath:  defaultPath,
		Status:    "pending",
	}

	transferMu.Lock()
	activeTransfer = transfer
	transferMu.Unlock()

	var pathInput *tview.InputField

	contentView := tview.NewTextView()
	contentView.SetBackgroundColor(ColorBg)
	contentView.SetTextColor(ColorFg)
	contentView.SetDynamicColors(true)
	contentView.SetTextAlign(tview.AlignCenter)
	contentView.SetText(fmt.Sprintf("\n📄 [white]%s[default] from [yellow]%s[default]\nSize: %s\n",
		filename, sender, formatFileSize(size)))

	pathInput = tview.NewInputField()
	pathInput.SetLabel("Save to: ")
	pathInput.SetText(defaultPath)
	pathInput.SetFieldWidth(40)
	pathInput.SetBackgroundColor(ColorBg)
	pathInput.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	pathInput.SetFieldTextColor(ColorFg)
	pathInput.SetLabelColor(ColorHighlight)

	expiresLabel := tview.NewTextView()
	expiresLabel.SetBackgroundColor(ColorBg)
	expiresLabel.SetTextColor(ColorFg)
	expiresLabel.SetTextAlign(tview.AlignCenter)
	expiresLabel.SetDynamicColors(true)

	expiresIn := 300

	updateExpiry := func() {
		expiresLabel.SetText(fmt.Sprintf("⏱ Expires in %d:%02d", expiresIn/60, expiresIn%60))
	}
	updateExpiry()

	// Buttons
	form := tview.NewForm()
	form.SetBackgroundColor(ColorBg)
	form.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	form.SetButtonTextColor(ColorTitle)

	form.AddButton("Change", func() {
		a.showFileBrowser(FileBrowserModeSave, filepath.Dir(pathInput.GetText()), filename, func(result FileBrowserResult) {
			if result.Selected {
				pathInput.SetText(result.Path)
				transfer.SavePath = result.Path
			}
			a.app.SetFocus(form)
		})
	})

	form.AddButton("Accept", func() {
		transfer.SavePath = pathInput.GetText()

		// Check if directory exists
		dir := filepath.Dir(transfer.SavePath)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				a.showErrorDialog("Error", fmt.Sprintf("Cannot create directory: %v", err))
				return
			}
		}

		a.pages.RemovePage("receivefile")
		a.client.AcceptFile(sender, sessionID)
	})

	form.AddButton("Decline", func() {
		transfer.Status = "cancelled"
		a.client.DeclineFile(sender, sessionID, "")
		transferMu.Lock()
		activeTransfer = nil
		transferMu.Unlock()
		a.pages.RemovePage("receivefile")
		if a.messageInput != nil {
			a.app.SetFocus(a.messageInput)
		} else {
			a.app.SetFocus(a.contactsList)
		}
	})

	// Layout
	innerFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(contentView, 4, 0, false).
		AddItem(pathInput, 1, 0, false).
		AddItem(nil, 1, 0, false).
		AddItem(form, 1, 0, true).
		AddItem(nil, 1, 0, false).
		AddItem(expiresLabel, 1, 0, false)
	innerFlex.SetBackgroundColor(ColorBg)
	innerFlex.SetBorder(true)
	innerFlex.SetBorderColor(ColorBorder)
	innerFlex.SetTitle(" Incoming File ")
	innerFlex.SetTitleColor(ColorTitle)

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(innerFlex, 50, 0, true).
			AddItem(nil, 0, 1, false), 12, 0, true).
		AddItem(nil, 0, 1, false)
	mainFlex.SetBackgroundColor(ColorBg)

	mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			transfer.Status = "cancelled"
			a.client.DeclineFile(sender, sessionID, "")
			transferMu.Lock()
			activeTransfer = nil
			transferMu.Unlock()
			a.pages.RemovePage("receivefile")
			if a.messageInput != nil {
				a.app.SetFocus(a.messageInput)
			} else {
				a.app.SetFocus(a.contactsList)
			}
			return nil
		}
		return event
	})

	a.pages.AddPage("receivefile", mainFlex, true, true)
	a.app.SetFocus(form)

	// Expiry countdown
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			transfer.mu.Lock()
			status := transfer.Status
			transfer.mu.Unlock()

			if status != "pending" {
				return
			}

			expiresIn--
			if expiresIn <= 0 {
				transfer.Status = "failed"
				transfer.Error = "timeout"
				transferMu.Lock()
				activeTransfer = nil
				transferMu.Unlock()
				a.app.QueueUpdateDraw(func() {
					a.pages.RemovePage("receivefile")
				})
				return
			}

			a.app.QueueUpdateDraw(updateExpiry)
		}
	}()
}

// showReceiveProgressDialog shows the progress dialog for receiving
func (a *App) showReceiveProgressDialog(transfer *FileTransfer) {
	transfer.Status = "transferring"
	transfer.StartTime = time.Now()

	contentView := tview.NewTextView()
	contentView.SetBackgroundColor(ColorBg)
	contentView.SetTextColor(ColorFg)
	contentView.SetDynamicColors(true)
	contentView.SetTextAlign(tview.AlignCenter)
	contentView.SetBorder(true)
	contentView.SetBorderColor(ColorBorder)
	contentView.SetTitle(" Receiving File ")
	contentView.SetTitleColor(ColorTitle)

	updateProgress := func() {
		transfer.mu.Lock()
		bytesDone := transfer.BytesDone
		status := transfer.Status
		transfer.mu.Unlock()

		if status != "transferring" {
			return
		}

		percent := 0
		if transfer.Size > 0 {
			percent = int(bytesDone * 100 / transfer.Size)
		}

		elapsed := time.Since(transfer.StartTime).Seconds()
		speed := float64(0)
		if elapsed > 0 {
			speed = float64(bytesDone) / elapsed
		}

		progressBar := buildProgressBar(percent, 30)

		content := fmt.Sprintf("\n📥 %s ← %s\n\n%s %d%%\n%s / %s  •  %s/s\n",
			transfer.Filename, transfer.Contact,
			progressBar, percent,
			formatFileSize(bytesDone), formatFileSize(transfer.Size),
			formatFileSize(int64(speed)))

		contentView.SetText(content)
	}

	updateProgress()

	// Cancel button
	cancelBtn := tview.NewButton("Cancel")
	cancelBtn.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	cancelBtn.SetLabelColor(ColorTitle)
	cancelBtn.SetSelectedFunc(func() {
		transfer.mu.Lock()
		transfer.Status = "cancelled"
		transfer.mu.Unlock()
		a.client.CancelFile(transfer.Contact, transfer.SessionID, "user cancelled")
		transferMu.Lock()
		activeTransfer = nil
		transferMu.Unlock()
		a.pages.RemovePage("receiveprogress")
		if a.messageInput != nil {
			a.app.SetFocus(a.messageInput)
		} else {
			a.app.SetFocus(a.contactsList)
		}
	})

	buttonFlex := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(cancelBtn, 10, 0, true).
		AddItem(nil, 0, 1, false)

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(contentView, 8, 0, false).
				AddItem(buttonFlex, 1, 0, true), 50, 0, true).
			AddItem(nil, 0, 1, false), 11, 0, true).
		AddItem(nil, 0, 1, false)
	mainFlex.SetBackgroundColor(ColorBg)

	mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			transfer.mu.Lock()
			transfer.Status = "cancelled"
			transfer.mu.Unlock()
			a.client.CancelFile(transfer.Contact, transfer.SessionID, "user cancelled")
			transferMu.Lock()
			activeTransfer = nil
			transferMu.Unlock()
			a.pages.RemovePage("receiveprogress")
			if a.messageInput != nil {
				a.app.SetFocus(a.messageInput)
			} else {
				a.app.SetFocus(a.contactsList)
			}
			return nil
		}
		return event
	})

	a.pages.AddPage("receiveprogress", mainFlex, true, true)
	a.app.SetFocus(cancelBtn)

	// Start the actual file transfer in background
	go a.performReceiveTransfer(transfer, func() {
		a.app.QueueUpdateDraw(updateProgress)
	})

	// Update progress periodically
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			transfer.mu.Lock()
			status := transfer.Status
			transfer.mu.Unlock()

			if status != "transferring" {
				return
			}

			a.app.QueueUpdateDraw(updateProgress)
		}
	}()
}

// performReceiveTransfer performs the actual file receive
func (a *App) performReceiveTransfer(transfer *FileTransfer, onProgress func()) {
	// Create file
	file, err := os.Create(transfer.SavePath)
	if err != nil {
		transfer.mu.Lock()
		transfer.Status = "failed"
		transfer.Error = err.Error()
		transfer.mu.Unlock()
		a.showTransferResult(transfer)
		return
	}
	defer file.Close()

	// Connect to download port
	serverAddr := a.client.GetServerAddr()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", serverAddr, transfer.Port), 30*time.Second)
	if err != nil {
		transfer.mu.Lock()
		transfer.Status = "failed"
		transfer.Error = err.Error()
		transfer.mu.Unlock()
		a.showTransferResult(transfer)
		return
	}
	defer conn.Close()

	// Create progress writer
	writer := &progressWriter{
		writer: file,
		onProgress: func(n int64) {
			transfer.mu.Lock()
			transfer.BytesDone = n
			transfer.mu.Unlock()
			onProgress()
		},
	}

	// Copy from connection to file
	_, err = io.Copy(writer, conn)

	transfer.mu.Lock()
	if err != nil && transfer.Status == "transferring" {
		transfer.Status = "failed"
		transfer.Error = err.Error()
	} else if transfer.Status == "transferring" {
		transfer.Status = "completed"
	}
	transfer.mu.Unlock()

	// Verify hash if provided
	if transfer.Status == "completed" && transfer.Hash != "" {
		file.Close() // Close before reading for hash
		hash, err := calculateFileHash(transfer.SavePath)
		if err != nil {
			transfer.mu.Lock()
			transfer.Error = "hash verification failed: " + err.Error()
			transfer.mu.Unlock()
		} else if hash != transfer.Hash {
			transfer.mu.Lock()
			transfer.Error = "hash mismatch"
			transfer.mu.Unlock()
		}
	}

	a.showTransferResult(transfer)
}

// showTransferResult shows the result dialog
func (a *App) showTransferResult(transfer *FileTransfer) {
	transferMu.Lock()
	activeTransfer = nil
	transferMu.Unlock()

	a.app.QueueUpdateDraw(func() {
		// Remove progress dialogs
		a.pages.RemovePage("sendprogress")
		a.pages.RemovePage("sendwaiting")
		a.pages.RemovePage("receiveprogress")

		transfer.mu.Lock()
		status := transfer.Status
		errorMsg := transfer.Error
		direction := transfer.Direction
		filename := transfer.Filename
		size := transfer.Size
		savePath := transfer.SavePath
		hash := transfer.Hash
		startTime := transfer.StartTime
		transfer.mu.Unlock()

		elapsed := time.Since(startTime)

		var content string
		var title string

		if direction == "send" {
			title = " Send File "
			if status == "completed" {
				content = fmt.Sprintf("\n✓ %s sent successfully\n%s in %.1f seconds\n",
					filename, formatFileSize(size), elapsed.Seconds())
			} else if status == "cancelled" {
				content = fmt.Sprintf("\n✗ Transfer cancelled\n%s\n", filename)
			} else {
				content = fmt.Sprintf("\n✗ Transfer failed\n%s\n%s\n", filename, errorMsg)
			}
		} else {
			title = " Receive File "
			if status == "completed" {
				hashStatus := "— not provided"
				if hash != "" {
					if errorMsg == "hash mismatch" {
						hashStatus = "✗ mismatch (file may be corrupted)"
					} else if errorMsg != "" && strings.Contains(errorMsg, "hash") {
						hashStatus = "✗ verification failed"
					} else {
						hashStatus = "✓ verified"
					}
				}
				content = fmt.Sprintf("\n✓ %s received\n%s in %.1f seconds\nHash: %s\nSaved to: %s\n",
					filename, formatFileSize(size), elapsed.Seconds(), hashStatus, savePath)
			} else if status == "cancelled" {
				content = fmt.Sprintf("\n✗ Transfer cancelled\n%s\n", filename)
			} else {
				content = fmt.Sprintf("\n✗ Transfer failed\n%s\n%s\n", filename, errorMsg)
			}
		}

		contentView := tview.NewTextView()
		contentView.SetBackgroundColor(ColorBg)
		contentView.SetTextColor(ColorFg)
		contentView.SetDynamicColors(true)
		contentView.SetTextAlign(tview.AlignCenter)
		contentView.SetText(content)
		contentView.SetBorder(true)
		contentView.SetBorderColor(ColorBorder)
		contentView.SetTitle(title)
		contentView.SetTitleColor(ColorTitle)

		okBtn := tview.NewButton("OK")
		okBtn.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
		okBtn.SetLabelColor(ColorTitle)
		okBtn.SetSelectedFunc(func() {
			a.pages.RemovePage("transferresult")
			if a.messageInput != nil {
				a.app.SetFocus(a.messageInput)
			} else {
				a.app.SetFocus(a.contactsList)
			}
		})

		buttonFlex := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(okBtn, 6, 0, true).
			AddItem(nil, 0, 1, false)

		height := 10
		if direction == "receive" && status == "completed" {
			height = 12
		}

		mainFlex := tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().
				AddItem(nil, 0, 1, false).
				AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
					AddItem(contentView, height-2, 0, false).
					AddItem(buttonFlex, 1, 0, true), 50, 0, true).
				AddItem(nil, 0, 1, false), height, 0, true).
			AddItem(nil, 0, 1, false)
		mainFlex.SetBackgroundColor(ColorBg)

		mainFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyEsc {
				a.pages.RemovePage("transferresult")
				if a.messageInput != nil {
					a.app.SetFocus(a.messageInput)
				} else {
					a.app.SetFocus(a.contactsList)
				}
				return nil
			}
			return event
		})

		a.pages.AddPage("transferresult", mainFlex, true, true)
		a.app.SetFocus(okBtn)
	})
}

// showErrorDialog shows a simple error dialog
func (a *App) showErrorDialog(title, message string) {
	modal := tview.NewModal()
	modal.SetText(message)
	modal.SetBackgroundColor(ColorBg)
	modal.SetTextColor(ColorFg)
	modal.SetButtonBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	modal.SetButtonTextColor(ColorTitle)
	modal.AddButtons([]string{"OK"})
	modal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		a.pages.RemovePage("errordialog")
		if a.messageInput != nil {
			a.app.SetFocus(a.messageInput)
		} else if a.contactsList != nil {
			a.app.SetFocus(a.contactsList)
		}
	})

	a.pages.AddPage("errordialog", modal, true, true)
}

// buildProgressBar builds a text progress bar
func buildProgressBar(percent, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := width * percent / 100
	empty := width - filled

	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", empty) + "]"
}

// calculateFileHash calculates SHA256 hash of a file
func calculateFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	reader     io.Reader
	total      int64
	onProgress func(int64)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	pr.total += int64(n)
	if pr.onProgress != nil {
		pr.onProgress(pr.total)
	}
	return
}

// progressWriter wraps an io.Writer to track progress
type progressWriter struct {
	writer     io.Writer
	total      int64
	onProgress func(int64)
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.writer.Write(p)
	pw.total += int64(n)
	if pw.onProgress != nil {
		pw.onProgress(pw.total)
	}
	return
}

// handleFileTransferResponse handles fsnd response (ok|fsnd|session_id|expires_in)
func (a *App) handleFileSendResponse(sessionID string, expiresIn int) {
	transferMu.Lock()
	if activeTransfer != nil && activeTransfer.Direction == "send" && activeTransfer.Status == "waiting" {
		activeTransfer.SessionID = sessionID
	}
	transferMu.Unlock()
}

// handleFileAccepted handles facc notification (recipient accepted, here's upload port)
func (a *App) handleFileAccepted(recipient, sessionID string, port int) {
	transferMu.Lock()
	transfer := activeTransfer
	transferMu.Unlock()

	if transfer == nil || transfer.SessionID != sessionID {
		return
	}

	transfer.mu.Lock()
	transfer.Port = port
	transfer.mu.Unlock()

	a.app.QueueUpdateDraw(func() {
		a.pages.RemovePage("sendwaiting")
		a.showSendProgressDialog(transfer)
	})
}

// handleFileAcceptResponse handles ok|facc|download_port response
func (a *App) handleFileAcceptResponse(port int) {
	transferMu.Lock()
	transfer := activeTransfer
	transferMu.Unlock()

	if transfer == nil || transfer.Direction != "receive" {
		return
	}

	transfer.mu.Lock()
	transfer.Port = port
	transfer.mu.Unlock()

	a.app.QueueUpdateDraw(func() {
		a.showReceiveProgressDialog(transfer)
	})
}

// handleFileDeclined handles fdec notification
func (a *App) handleFileDeclined(user, sessionID, reason string) {
	transferMu.Lock()
	transfer := activeTransfer
	transferMu.Unlock()

	if transfer == nil || transfer.SessionID != sessionID {
		return
	}

	transfer.mu.Lock()
	transfer.Status = "failed"
	if reason != "" {
		transfer.Error = "Declined: " + reason
	} else {
		transfer.Error = "Declined by recipient"
	}
	transfer.mu.Unlock()

	a.showTransferResult(transfer)
}

// handleFileCancelled handles fcan notification
func (a *App) handleFileCancelled(user, sessionID, reason string) {
	transferMu.Lock()
	transfer := activeTransfer
	transferMu.Unlock()

	if transfer == nil || transfer.SessionID != sessionID {
		return
	}

	transfer.mu.Lock()
	transfer.Status = "cancelled"
	if reason != "" {
		transfer.Error = "Cancelled: " + reason
	} else {
		transfer.Error = "Cancelled by " + user
	}
	transfer.mu.Unlock()

	a.showTransferResult(transfer)
}

// handleIncomingFile handles incoming fsnd notification
func (a *App) handleIncomingFile(sender, filename string, size int64, hash, sessionID string) {
	a.app.QueueUpdateDraw(func() {
		a.showReceiveFileDialog(sender, filename, size, hash, sessionID)
	})
}

// getActiveTransfer returns the current active transfer (for handlers)
func getActiveTransfer() *FileTransfer {
	transferMu.Lock()
	defer transferMu.Unlock()
	return activeTransfer
}

// parseFileSize parses size string to int64
func parseFileSize(s string) int64 {
	size, _ := strconv.ParseInt(s, 10, 64)
	return size
}

