package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// chatRequest mirrors master/lib.ChatRequest.
type chatRequest struct {
	UserID string `json:"userId"`
	Prompt string `json:"prompt"`
	Tier   string `json:"tier"`
}

// chatResponse mirrors master/lib.ChatResponse.
type chatResponse struct {
	RequestID string `json:"requestId"`
	Reply     string `json:"reply"`
}

// sendChat posts a chat request to the master and returns the response.
// It is safe to call from a tea.Cmd goroutine.
func sendChat(ctx context.Context, masterURL, userID, tier, prompt string) (chatResponse, error) {
	payload, err := json.Marshal(chatRequest{
		UserID: userID,
		Prompt: prompt,
		Tier:   tier,
	})
	if err != nil {
		return chatResponse{}, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, masterURL+"/chat", bytes.NewReader(payload))
	if err != nil {
		return chatResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return chatResponse{}, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return chatResponse{}, fmt.Errorf("server error %d: %s", resp.StatusCode, buf.String())
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return chatResponse{}, fmt.Errorf("decode: %w", err)
	}
	return cr, nil
}
