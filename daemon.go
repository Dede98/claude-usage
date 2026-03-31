package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func runDaemonMode() {
	interval := 60
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = n
		}
	}

	home, _ := os.UserHomeDir()
	pidFile := filepath.Join(home, ".claude", "usage-daemon.pid")

	os.MkdirAll(filepath.Dir(pidFile), 0755)
	os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sig
		os.Remove(pidFile)
		os.Exit(0)
	}()

	for {
		data := pingAPI()
		writeUsageFile(data)
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

func runWatch() {
	interval := 60
	if v := os.Getenv("POLL_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = n
		}
	}

	for {
		// Clear screen
		fmt.Print("\033[2J\033[H")

		data := pingAPI()
		writeUsageFile(data)
		formatDisplay(data)

		fmt.Printf("%s  Refreshes every %ds · Ctrl+C to stop%s\n", colorDim, interval, colorReset)
		time.Sleep(time.Duration(interval) * time.Second)
	}
}
