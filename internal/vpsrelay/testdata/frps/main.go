// A separate frps process for native transport tests, without dashboard assets.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/server"
)

func main() {
	path := flag.String("c", "", "fixture JSON config")
	flag.Parse()
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg v1.ServerConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return err
	}
	cfg.Complete()
	s, err := server.NewService(&cfg)
	if err != nil {
		return err
	}
	defer s.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	s.Run(ctx)
	return nil
}
