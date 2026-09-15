//go:build evals

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// session is a JSON-RPC conversation with the built server over stdio.
//
// The harness needs its own session, separate from the model's, for two
// reasons. It reads the world back to score an end state, and a model's
// account of what it did is the least reliable thing in the run. And it
// creates and deletes the sandbox, which needs the delete tools — which
// the MODEL's session deliberately does not have.
type session struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	stdin  *bufio.Writer
	stdout *bufio.Reader
	nextID int
}

const (
	callTimeout = 90 * time.Second
	stopTimeout = 5 * time.Second
)

func start(bin string, env ...string) (*session, error) {
	cmd := exec.Command(bin)
	cmd.Env = append(environ(), env...)
	cmd.Stderr = nil

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &session{cmd: cmd, in: in, stdin: bufio.NewWriter(in), stdout: bufio.NewReader(out), nextID: 1}
	if err := s.initialize(); err != nil {
		return nil, err
	}
	return s, nil
}

// stop closes the WRITE end of the server's stdin, which is what it
// needs to see EOF and exit. cmd.Stdin is the read end; closing that
// leaves the server running and Wait blocking for ever, which is the
// bug the live driver shipped with for a phase.
func (s *session) stop() {
	_ = s.stdin.Flush()
	_ = s.in.Close()
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(stopTimeout):
		_ = s.cmd.Process.Kill()
		<-done
	}
}

func (s *session) initialize() error {
	if _, err := s.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "favro-mcp-evals", "version": "0"},
	}); err != nil {
		return err
	}
	return s.notify("notifications/initialized")
}

// Call invokes one tool and returns its structuredContent.
func (s *session) Call(tool string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := s.request("tools/call", map[string]any{"name": tool, "arguments": args})
	if err != nil {
		return nil, err
	}
	var res struct {
		IsError           bool                    `json:"isError"`
		StructuredContent map[string]any          `json:"structuredContent"`
		Content           []struct{ Text string } `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("%s: %w", tool, err)
	}
	if res.IsError {
		msg := ""
		if len(res.Content) > 0 {
			msg = res.Content[0].Text
		}
		return nil, fmt.Errorf("%s: %s", tool, msg)
	}
	return res.StructuredContent, nil
}

func (s *session) request(method string, params any) (json.RawMessage, error) {
	id := s.nextID
	s.nextID++
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(callTimeout)
	for time.Now().Before(deadline) {
		line, err := s.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("%s: %w", method, err)
		}
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &msg); err != nil || msg.ID == nil || *msg.ID != id {
			continue // a notification, or a frame for someone else
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s (%d)", method, msg.Error.Message, msg.Error.Code)
		}
		return msg.Result, nil
	}
	return nil, fmt.Errorf("%s: no answer within %s", method, callTimeout)
}

func (s *session) notify(method string) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (s *session) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := s.stdin.Write(append(body, '\n')); err != nil {
		return err
	}
	return s.stdin.Flush()
}
