// Command plugin is the Kandev Slack plugin backend. Kandev spawns it over
// the go-plugin handshake and supervises it; it is not meant to be run by
// hand except for a build smoke check.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/kdlbs/kandev/pluginsdk"
)

func main() {
	// Logs go to stderr: stdout carries the go-plugin handshake.
	log.SetOutput(os.Stderr)
	log.SetPrefix("kandev-plugin-slack: ")
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pluginsdk.Serve(newSlackPlugin(ctx))
}
