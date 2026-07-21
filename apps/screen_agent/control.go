package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	controlRequestLimit = 4096
	controlTimeout      = 2 * time.Second
)

type controlRequest struct {
	Intent AssistIntent `json:"intent"`
}

type controlResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type controlDispatch struct {
	Intent   AssistIntent
	response chan error
}

func (d controlDispatch) respond(err error) {
	select {
	case d.response <- err:
	default:
	}
}

type ControlServer struct {
	listener *net.UnixListener
	requests chan controlDispatch
	errors   chan error

	closeOnce sync.Once
	wg        sync.WaitGroup
}

func ControlSocketPath(getenv func(string) string) string {
	return filepath.Join(RuntimeDir(getenv), "screen-agent.sock")
}

func StartControlServer(ctx context.Context, getenv func(string) string) (*ControlServer, error) {
	path := ControlSocketPath(getenv)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create control socket directory: %w", err)
	}
	if err := prepareControlSocket(path); err != nil {
		return nil, err
	}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on control socket %q: %w", path, err)
	}
	listener.SetUnlinkOnClose(true)
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("secure control socket %q: %w", path, err)
	}

	server := &ControlServer{
		listener: listener,
		requests: make(chan controlDispatch, 8),
		errors:   make(chan error, 1),
	}
	server.wg.Add(1)
	go server.serve(ctx)
	return server, nil
}

func prepareControlSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect control socket %q: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("control socket path %q exists and is not a socket", path)
	}

	conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("screen-agent daemon is already listening on %q", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale control socket %q: %w", path, err)
	}
	return nil
}

func (s *ControlServer) Requests() <-chan controlDispatch {
	return s.requests
}

func (s *ControlServer) Errors() <-chan error {
	return s.errors
}

func (s *ControlServer) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.listener.Close()
		s.wg.Wait()
	})
	if errors.Is(closeErr, net.ErrClosed) {
		return nil
	}
	return closeErr
}

func (s *ControlServer) serve(ctx context.Context) {
	defer s.wg.Done()
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			select {
			case s.errors <- err:
			default:
			}
			return
		}
		s.handleConnection(ctx, conn)
	}
}

func (s *ControlServer) handleConnection(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(controlTimeout))

	request, err := decodeControlRequest(conn)
	if err != nil {
		_ = json.NewEncoder(conn).Encode(controlResponse{Error: err.Error()})
		return
	}
	if ctx.Err() != nil {
		_ = json.NewEncoder(conn).Encode(controlResponse{Error: "screen-agent daemon is stopping"})
		return
	}

	dispatch := controlDispatch{Intent: request.Intent, response: make(chan error, 1)}
	select {
	case s.requests <- dispatch:
	case <-ctx.Done():
		_ = json.NewEncoder(conn).Encode(controlResponse{Error: "screen-agent daemon is stopping"})
		return
	}

	select {
	case err := <-dispatch.response:
		if err != nil {
			_ = json.NewEncoder(conn).Encode(controlResponse{Error: err.Error()})
			return
		}
		_ = json.NewEncoder(conn).Encode(controlResponse{OK: true})
	case <-ctx.Done():
		_ = json.NewEncoder(conn).Encode(controlResponse{Error: "screen-agent daemon is stopping"})
	}
}

func decodeControlRequest(r io.Reader) (controlRequest, error) {
	limited := &io.LimitedReader{R: r, N: controlRequestLimit + 1}
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()

	var request controlRequest
	if err := dec.Decode(&request); err != nil {
		return controlRequest{}, fmt.Errorf("decode control request: %w", err)
	}
	if limited.N == 0 {
		return controlRequest{}, fmt.Errorf("control request exceeds %d bytes", controlRequestLimit)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return controlRequest{}, err
	}
	if limited.N == 0 {
		return controlRequest{}, fmt.Errorf("control request exceeds %d bytes", controlRequestLimit)
	}
	if strings.TrimSpace(string(request.Intent)) == "" {
		return controlRequest{}, fmt.Errorf("control request intent is required")
	}
	intent, err := ParseAssistIntent(string(request.Intent))
	if err != nil {
		return controlRequest{}, err
	}
	request.Intent = intent
	return request, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var trailing any
	err := dec.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("control request must contain exactly one JSON object")
	}
	return fmt.Errorf("decode trailing control request data: %w", err)
}

func SendAssistIntent(ctx context.Context, getenv func(string) string, intent AssistIntent) error {
	intent, err := ParseAssistIntent(string(intent))
	if err != nil {
		return err
	}
	if getenv == nil {
		getenv = os.Getenv
	}

	requestCtx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(requestCtx, "unix", ControlSocketPath(getenv))
	if err != nil {
		return fmt.Errorf("contact screen-agent daemon: %w; start it with `screen-agent daemon`", err)
	}
	defer conn.Close()
	if deadline, ok := requestCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if err := json.NewEncoder(conn).Encode(controlRequest{Intent: intent}); err != nil {
		return fmt.Errorf("send %s request: %w", intent.displayName(), err)
	}
	if unixConn, ok := conn.(*net.UnixConn); ok {
		if err := unixConn.CloseWrite(); err != nil {
			return fmt.Errorf("finish %s request: %w", intent.displayName(), err)
		}
	}
	var response controlResponse
	if err := json.NewDecoder(io.LimitReader(conn, controlRequestLimit)).Decode(&response); err != nil {
		return fmt.Errorf("read screen-agent daemon response: %w", err)
	}
	if !response.OK {
		message := strings.TrimSpace(response.Error)
		if message == "" {
			message = "request rejected"
		}
		return fmt.Errorf("screen-agent daemon rejected %s request: %s", intent.displayName(), message)
	}
	return nil
}
