package mcp

import (
	"bufio"
	"context"
	"io"
)

// MessageHandler can dispatch MCP requests locally or relay them to a daemon.
type MessageHandler interface {
	HandleMessage(context.Context, []byte) ([]byte, error)
}

// RunStdio reads newline-delimited JSON-RPC and writes one response per line.
func RunStdio(ctx context.Context, server MessageHandler, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	// Allow large messages (e.g. feeds with lots of episodes)
	const maxScanTokenSize = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		respBytes, err := server.HandleMessage(ctx, line)
		if err != nil {
			return err
		}

		if len(respBytes) > 0 {
			if _, err := out.Write(respBytes); err != nil {
				return err
			}
			if _, err := out.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}
