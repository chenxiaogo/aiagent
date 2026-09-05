package main

import (
	"context"
	"log"
	"syscall"
	"time"

	"aiagent/pkg/app"
	"aiagent/pkg/shutdown"
)

func main() {
	hook := shutdown.NewHook().
		WithSignals(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP).
		WithTimeout(60 * time.Second)

	hook.AddCloseFunc(func(ctx context.Context) {
		log.Println("cleaning up aiagent resources...")
	})

	if err := hook.Run(app.NewServer); err != nil {
		log.Fatalf("application failed: %v", err)
	}
	log.Println("application exited successfully")
}
