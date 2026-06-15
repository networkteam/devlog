package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

const sessionPollInterval = 2 * time.Second

// selectSession resolves the capture session to attach to. An explicit sid is
// used as-is. Otherwise: a single active session is auto-selected, several prompt
// interactively, and none triggers a wait until one appears.
func selectSession(ctx context.Context, baseURL, sid string) (string, error) {
	if sid != "" {
		return sid, nil
	}

	httpc := defaultHTTPClient()
	waited := false
	for {
		sessions, err := fetchSessions(ctx, baseURL, httpc)
		if err != nil {
			return "", fmt.Errorf("listing devlog sessions (is the dashboard running with the agent API enabled?): %w", err)
		}

		switch {
		case len(sessions) == 1:
			s := sessions[0]
			fmt.Printf("Attaching to devlog session %s (%s, %d events)\n", s.SessionID, s.Mode, s.EventCount)
			return s.SessionID, nil
		case len(sessions) > 1:
			return promptSession(sessions)
		default:
			if !waited {
				fmt.Println("No active devlog session. Open the dashboard and start capturing — waiting…")
				waited = true
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(sessionPollInterval):
			}
		}
	}
}

// promptSession asks the user to choose among several active sessions. Without a
// terminal it errors and asks for an explicit --session.
func promptSession(sessions []sessionInfo) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("%d active sessions; re-run with --session <sid> to choose", len(sessions))
	}

	fmt.Println("Multiple active devlog sessions:")
	for i, s := range sessions {
		fmt.Printf("  [%d] %s  (%s, %d events, active %ds ago)\n",
			i+1, s.SessionID, s.Mode, s.EventCount, s.LastActiveMsAgo/1000)
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("Attach to which session? [1-%d]: ", len(sessions))
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || n < 1 || n > len(sessions) {
			fmt.Println("Please enter a valid number.")
			continue
		}
		return sessions[n-1].SessionID, nil
	}
}
