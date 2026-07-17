package main

import (
	"context"
	"fmt"
	"os"

	"github.com/nickdu2009/worktrail/internal/app"
)

type exitCoder interface {
	ExitCode() int
}

func main() {
	if err := app.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if ec, ok := err.(exitCoder); ok {
			if msg := err.Error(); msg != "" {
				fmt.Fprintln(os.Stderr, msg)
			}
			os.Exit(ec.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
