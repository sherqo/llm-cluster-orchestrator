package loadtui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type chatRequest struct {
	UserID string `json:"userId"`
	Prompt string `json:"prompt"`
	Tier   string `json:"tier"`
}

type chatResponse struct {
	RequestID string `json:"requestId"`
	Reply     string `json:"reply"`
}

func sendChat(ctx context.Context, masterURL, userID, tier, prompt string) (chatResponse, error) {
	payload, err := json.Marshal(chatRequest{UserID: userID, Prompt: prompt, Tier: tier})
	if err != nil {
		return chatResponse{}, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, masterURL+"/chat", bytes.NewReader(payload))
	if err != nil {
		return chatResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return chatResponse{}, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return chatResponse{}, fmt.Errorf("server error %d", resp.StatusCode)
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return chatResponse{}, fmt.Errorf("decode: %w", err)
	}
	return cr, nil
}
