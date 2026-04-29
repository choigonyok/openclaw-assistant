package main

import (
	"context"
	"fmt"
	"os"

	"openclaw-assistant/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
