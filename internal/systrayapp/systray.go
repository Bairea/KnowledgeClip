package systrayapp

import (
	_ "embed"
	"fmt"
	"log"
	"os/exec"
	"runtime"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var iconData []byte

var actualPort int

// SetPort sets the actual port to use
func SetPort(port int) {
	actualPort = port
}

// Run starts the system tray
func Run(onExit func()) {
	systray.Run(func() {
		onReady()
	}, onExit)
}

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle(fmt.Sprintf("KnowledgeClip (port: %d)", actualPort))
	systray.SetTooltip("KnowledgeClip - Multi-site Chat Aggregator")

	mOpen := systray.AddMenuItem("Open Interface", "Open interface in browser")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Exit", "Close program")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				openBrowser(actualPort)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	// Auto-open browser on startup
	go func() {
		openBrowser(actualPort)
	}()
}

func openBrowser(port int) {
	url := fmt.Sprintf("http://localhost:%d", port)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}

	if err := cmd.Start(); err != nil {
		log.Printf("failed to open browser: %v", err)
	}
}
