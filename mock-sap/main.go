package main

import (
	"go.uber.org/fx"
)

func main() {
	fx.New(
		fx.Provide(LoadConfig, NewServer, NewRouter, NewHTTPServer),
		fx.Invoke(RegisterLifecycle),
	).Run()
}
