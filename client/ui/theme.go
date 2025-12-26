package ui

import "github.com/gdamore/tcell/v2"

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
