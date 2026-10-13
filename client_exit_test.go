package claudeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestClientExitHelper is a real subprocess that exits without acknowledging
// initialization or a later control request. It requires no CLI or credentials.
func TestClientExitHelper(t *testing.T) {
	mode := os.Getenv("CLAUDE_SDK_EXIT_HELPER")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		os.Exit(2)
	}
	if mode == "control" {
		var req SDKControlRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		_, _ = fmt.Fprintf(os.Stdout, `{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{}}}`+"\n", req.RequestID)
		if !scanner.Scan() {
			os.Exit(2)
		}
	}
	_, _ = fmt.Fprintln(os.Stderr, "No conversation found with session ID: missing-session")
	os.Exit(1)
}

type exitTestRunner struct {
	*LocalSubprocessRunner
	mode  string
	waits atomic.Int32
	done  chan error
}

func (r *exitTestRunner) Start(ctx context.Context, _ []string, env []string, cwd string) (io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	return r.LocalSubprocessRunner.Start(ctx,
		[]string{"-test.run=^TestClientExitHelper$"},
		append(env, "CLAUDE_SDK_EXIT_HELPER="+r.mode), cwd)
}

func (r *exitTestRunner) Wait() error {
	r.waits.Add(1)
	err := r.LocalSubprocessRunner.Wait()
	r.done <- err
	return err
}

func TestClientProcessExitReleasesControlWaiters(t *testing.T) {
	for _, mode := range []string{"initialize", "control"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			runner := &exitTestRunner{
				LocalSubprocessRunner: NewLocalSubprocessRunner(executable),
				mode:                  mode,
				done:                  make(chan error, 2),
			}
			transport := NewSubprocessTransportWithRunner(runner, NewOptions())
			client, err := NewClient(WithTransport(transport))
			require.NoError(t, err)
			// No deadline: subprocess EOF, not a timeout, must release the caller.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer client.Close()
			defer transport.Close()
			result := make(chan error, 1)
			go func() {
				stream, err := client.Stream(ctx)
				if err == nil && mode == "control" {
					err = stream.SetModel(ctx, "test-model")
				}
				result <- err
			}()
			select {
			case err := <-result:
				require.ErrorIs(t, err, io.EOF)
				require.False(t, errors.Is(err, context.Canceled))
			case <-time.After(3 * time.Second):
				cancel()
				<-result
				t.Fatal("CLI exited but the control request remained blocked")
			}
			select {
			case err := <-runner.done:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("CLI was not reaped")
			}
			require.NotNil(t, runner.cmd.ProcessState)
			require.Equal(t, int32(1), runner.waits.Load())
			require.False(t, transport.IsReady())
			client.protocol.pendingReqs.Range(func(_, _ any) bool {
				t.Error("control waiter leaked after exit")
				return true
			})
		})
	}
}

func TestProtocolInitializeRegistersBeforeWrite(t *testing.T) {
	transport := newStreamControlTransport(nil)
	protocol := NewProtocol(transport, NewOptions())
	// Deliver a response synchronously inside Write, before it returns.
	immediate := &immediateInitTransport{streamControlTransport: transport, protocol: protocol}
	protocol.transport = immediate
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, protocol.Initialize(ctx))
}

type immediateInitTransport struct {
	*streamControlTransport
	protocol *Protocol
}

func (t *immediateInitTransport) Write(_ context.Context, msg Message) error {
	req := msg.(SDKControlRequest)
	return t.protocol.handleSDKControlResponse(SDKControlResponse{
		Type: "control_response",
		Response: SDKControlResponseBody{
			Subtype: "success", RequestID: req.RequestID,
			Response: map[string]interface{}{},
		},
	})
}

func TestProtocolTerminationPreservesBufferedResponse(t *testing.T) {
	protocol := NewProtocol(newMockTransport(1), NewOptions())
	ch := make(chan SDKControlResponse, 1)
	ch <- SDKControlResponse{Type: "control_response"}
	protocol.terminate(io.EOF)
	resp, err := protocol.waitForSDKResponse(context.Background(), ch)
	require.NoError(t, err)
	require.Equal(t, "control_response", resp.Type)
	_, err = protocol.waitForSDKResponse(context.Background(), ch)
	require.ErrorIs(t, err, io.EOF)
}

func TestClientCloseReleasesControlWaiter(t *testing.T) {
	transport := newStreamControlTransport(nil)
	protocol := NewProtocol(transport, NewOptions())
	client := &Client{transport: transport, protocol: protocol, connected: true}
	stream := &Stream{client: client}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- stream.SetModel(ctx, "test-model") }()
	select {
	case <-transport.writeCh:
	case <-time.After(time.Second):
		t.Fatal("control request was not written")
	}
	require.NoError(t, client.Close())
	select {
	case err := <-result:
		var closed *ErrTransportClosed
		require.ErrorAs(t, err, &closed)
	case <-time.After(time.Second):
		t.Fatal("Close did not release the control request")
	}
}
