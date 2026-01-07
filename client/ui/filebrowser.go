package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// FileBrowserMode determines what the browser selects
type FileBrowserMode int

const (
	FileBrowserModeFile FileBrowserMode = iota
	FileBrowserModeDirectory
	FileBrowserModeSave
)

// FileBrowserResult contains the result of file browser dialog
type FileBrowserResult struct {
	Selected bool
	Path     string
}

// showFileBrowser shows a file browser dialog
func (a *App) showFileBrowser(mode FileBrowserMode, initialPath string, filename string, callback func(FileBrowserResult)) {
	// Determine starting directory
	startDir := initialPath
	if startDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			startDir = "/"
		} else {
			startDir = home
		}
	}

	// If initialPath is a file, use its directory
	if info, err := os.Stat(startDir); err == nil && !info.IsDir() {
		startDir = filepath.Dir(startDir)
	}

	currentDir := startDir
	var fileList *tview.List
	var pathInput *tview.InputField
	var filenameInput *tview.InputField
	var statusText *tview.TextView

	// Create the file list
	fileList = tview.NewList()
	fileList.SetBorder(true)
	fileList.SetBorderColor(ColorBorder)
	fileList.SetBackgroundColor(ColorBg)
	fileList.SetMainTextColor(ColorFg)
	fileList.SetSecondaryTextColor(tcell.NewRGBColor(128, 128, 128))
	fileList.SetSelectedTextColor(ColorTitle)
	fileList.SetSelectedBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	fileList.SetHighlightFullLine(true)
	fileList.ShowSecondaryText(true)

	// Path input field
	pathInput = tview.NewInputField()
	pathInput.SetLabel(" Path: ")
	pathInput.SetFieldWidth(0)
	pathInput.SetBackgroundColor(ColorBg)
	pathInput.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
	pathInput.SetFieldTextColor(ColorFg)
	pathInput.SetLabelColor(ColorHighlight)
	pathInput.SetText(currentDir)

	// Filename input (for save mode)
	if mode == FileBrowserModeSave {
		filenameInput = tview.NewInputField()
		filenameInput.SetLabel(" Name: ")
		filenameInput.SetFieldWidth(0)
		filenameInput.SetBackgroundColor(ColorBg)
		filenameInput.SetFieldBackgroundColor(tcell.NewRGBColor(0, 0, 64))
		filenameInput.SetFieldTextColor(ColorFg)
		filenameInput.SetLabelColor(ColorHighlight)
		filenameInput.SetText(filename)
	}

	// Status text
	statusText = tview.NewTextView()
	statusText.SetBackgroundColor(tcell.NewRGBColor(0, 128, 128))
	statusText.SetTextColor(ColorTitle)
	statusText.SetTextAlign(tview.AlignCenter)
	if mode == FileBrowserModeSave {
		statusText.SetText(" Enter:Select | Tab:Switch | Esc:Cancel ")
	} else {
		statusText.SetText(" Enter:Select | Backspace:Up | Esc:Cancel ")
	}

	// Function to populate file list
	populateList := func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}

		fileList.Clear()

		// Add parent directory entry
		if dir != "/" {
			fileList.AddItem("📁 ..", "", 0, nil)
		}

		// Separate directories and files
		var dirs, files []os.DirEntry
		for _, entry := range entries {
			// Skip hidden files
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if entry.IsDir() {
				dirs = append(dirs, entry)
			} else {
				files = append(files, entry)
			}
		}

		// Sort alphabetically
		sort.Slice(dirs, func(i, j int) bool {
			return strings.ToLower(dirs[i].Name()) < strings.ToLower(dirs[j].Name())
		})
		sort.Slice(files, func(i, j int) bool {
			return strings.ToLower(files[i].Name()) < strings.ToLower(files[j].Name())
		})

		// Add directories first
		for _, entry := range dirs {
			name := entry.Name()
			fileList.AddItem(fmt.Sprintf("📁 %s/", name), "", 0, nil)
		}

		// Add files (only in file mode or save mode)
		if mode != FileBrowserModeDirectory {
			for _, entry := range files {
				name := entry.Name()
				info, err := entry.Info()
				sizeStr := ""
				if err == nil {
					sizeStr = formatFileSize(info.Size())
				}
				fileList.AddItem(fmt.Sprintf("📄 %s", name), sizeStr, 0, nil)
			}
		}

		currentDir = dir
		pathInput.SetText(dir)
		
		title := " Select File "
		if mode == FileBrowserModeDirectory {
			title = " Select Directory "
		} else if mode == FileBrowserModeSave {
			title = " Save As "
		}
		fileList.SetTitle(fmt.Sprintf("%s- %s ", title, dir))

		return nil
	}

	// Get entry name from list item text
	getEntryName := func(text string) string {
		// Remove icon prefix
		if strings.HasPrefix(text, "📁 ") {
			name := strings.TrimPrefix(text, "📁 ")
			return strings.TrimSuffix(name, "/")
		}
		if strings.HasPrefix(text, "📄 ") {
			return strings.TrimPrefix(text, "📄 ")
		}
		return text
	}

	// Check if entry is directory
	isDirectory := func(text string) bool {
		return strings.HasPrefix(text, "📁 ")
	}

	// Handle selection
	fileList.SetSelectedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		entryName := getEntryName(mainText)

		if entryName == ".." {
			// Go to parent directory
			parent := filepath.Dir(currentDir)
			if err := populateList(parent); err != nil {
				statusText.SetText(fmt.Sprintf(" Error: %v ", err))
			}
			return
		}

		fullPath := filepath.Join(currentDir, entryName)

		if isDirectory(mainText) {
			if mode == FileBrowserModeDirectory {
				// Select this directory
				a.pages.RemovePage("filebrowser")
				callback(FileBrowserResult{Selected: true, Path: fullPath})
			} else {
				// Enter directory
				if err := populateList(fullPath); err != nil {
					statusText.SetText(fmt.Sprintf(" Error: %v ", err))
				}
			}
		} else {
			// File selected
			if mode == FileBrowserModeFile {
				a.pages.RemovePage("filebrowser")
				callback(FileBrowserResult{Selected: true, Path: fullPath})
			} else if mode == FileBrowserModeSave {
				// In save mode, clicking a file puts its name in the input
				if filenameInput != nil {
					filenameInput.SetText(entryName)
					a.app.SetFocus(filenameInput)
				}
			}
		}
	})

	// Handle keyboard for file list
	fileList.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			a.pages.RemovePage("filebrowser")
			callback(FileBrowserResult{Selected: false})
			return nil
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			// Go to parent directory
			if currentDir != "/" {
				parent := filepath.Dir(currentDir)
				if err := populateList(parent); err != nil {
					statusText.SetText(fmt.Sprintf(" Error: %v ", err))
				}
			}
			return nil
		case tcell.KeyTab:
			if mode == FileBrowserModeSave && filenameInput != nil {
				a.app.SetFocus(filenameInput)
				return nil
			}
		}
		return event
	})

	// Handle filename input (save mode)
	if filenameInput != nil {
		filenameInput.SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEnter {
				name := filenameInput.GetText()
				if name != "" {
					fullPath := filepath.Join(currentDir, name)
					a.pages.RemovePage("filebrowser")
					callback(FileBrowserResult{Selected: true, Path: fullPath})
				}
			}
		})
		filenameInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			switch event.Key() {
			case tcell.KeyEsc:
				a.pages.RemovePage("filebrowser")
				callback(FileBrowserResult{Selected: false})
				return nil
			case tcell.KeyTab:
				a.app.SetFocus(fileList)
				return nil
			}
			return event
		})
	}

	// Handle path input
	pathInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			newPath := pathInput.GetText()
			if info, err := os.Stat(newPath); err == nil && info.IsDir() {
				if err := populateList(newPath); err != nil {
					statusText.SetText(fmt.Sprintf(" Error: %v ", err))
				}
			} else {
				statusText.SetText(" Invalid directory ")
			}
			a.app.SetFocus(fileList)
		}
	})

	// Initial population
	if err := populateList(currentDir); err != nil {
		statusText.SetText(fmt.Sprintf(" Error: %v ", err))
	}

	// Build layout
	var mainFlex *tview.Flex
	if mode == FileBrowserModeSave {
		mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(pathInput, 1, 0, false).
			AddItem(fileList, 0, 1, true).
			AddItem(filenameInput, 1, 0, false).
			AddItem(statusText, 1, 0, false)
	} else {
		mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(pathInput, 1, 0, false).
			AddItem(fileList, 0, 1, true).
			AddItem(statusText, 1, 0, false)
	}
	mainFlex.SetBackgroundColor(ColorBg)

	// Center the dialog
	dialogWidth := 60
	dialogHeight := 20

	// Create inner flex with background
	innerFlex := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(mainFlex, dialogHeight, 0, true).
		AddItem(nil, 0, 1, false)
	innerFlex.SetBackgroundColor(ColorBg)

	// Create outer centering flex
	centered := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(innerFlex, dialogWidth, 0, true).
		AddItem(nil, 0, 1, false)
	centered.SetBackgroundColor(ColorBg)

	a.pages.AddPage("filebrowser", centered, true, true)
	a.app.SetFocus(fileList)
}

// formatFileSize formats file size for display
func formatFileSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.1f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

