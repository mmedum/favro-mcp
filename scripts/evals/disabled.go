//go:build !evals

// The evals are behind a build tag so an ordinary `go build ./...`
// never tries to reach a network, an account or a model. The task table
// in tasks.go carries no tag, so `go test ./scripts/evals` walks every
// prompt without any of the three.
package main

import "fmt"

func main() {
	fmt.Println("evals needs a Favro organization, a network and the claude CLI, so it is behind a build tag.\n" +
		"Run it with:  make evals        (or: go run -tags=evals ./scripts/evals)")
}
