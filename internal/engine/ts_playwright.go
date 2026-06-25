package engine

import (
	"context"
	"errors"
	"os/exec"
	"runtime"

	"chat-aggregator/internal/models"
)

type TSPlaywrightEngine struct {
	cmd *exec.Cmd
}

func NewTSPlaywrightEngine() (*TSPlaywrightEngine, error) {
	cmdName := "npx"
	if runtime.GOOS == "windows" {
		_, err := exec.LookPath("npx.cmd")
		if err != nil {
			_, err = exec.LookPath("npx")
			if err != nil {
				return nil, errors.New("npx not found")
			}
		}
	} else {
		_, err := exec.LookPath("npx")
		if err != nil {
			return nil, errors.New("npx not found")
		}
	}

	return &TSPlaywrightEngine{cmd: exec.Command(cmdName)}, nil
}

func (te *TSPlaywrightEngine) SendMessage(ctx context.Context, site models.Site, prompt string) (string, error) {
	return "", errors.New("ts-playwright not yet implemented")
}

func (te *TSPlaywrightEngine) Close() error {
	if te.cmd != nil && te.cmd.Process != nil {
		return te.cmd.Process.Kill()
	}
	return nil
}
