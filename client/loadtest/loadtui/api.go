package loadtui

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
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

    client := &http.Client{Timeout: 180 * time.Second}
    maxRetries := 10
    backoff := 100 * time.Millisecond

    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        req, err := http.NewRequestWithContext(ctx, http.MethodPost, masterURL+"/chat", bytes.NewReader(payload))
        if err != nil {
            return chatResponse{}, fmt.Errorf("build request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")

        resp, err := client.Do(req)
        if err != nil {
            lastErr = fmt.Errorf("http: %w", err)
        } else {
            body, _ := io.ReadAll(resp.Body)
            resp.Body.Close()

            if resp.StatusCode == http.StatusOK {
                var cr chatResponse
                if err := json.NewDecoder(bytes.NewReader(body)).Decode(&cr); err != nil {
                    return chatResponse{}, fmt.Errorf("decode: %w", err)
                }
                return cr, nil
            }

            // Do not retry 4xx (except 429)
            if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
                return chatResponse{}, fmt.Errorf("server error %d: %s", resp.StatusCode, string(body))
            }

            lastErr = fmt.Errorf("server error %d: %s", resp.StatusCode, string(body))
        }

        select {
        case <-ctx.Done():
            return chatResponse{}, lastErr
        case <-time.After(backoff):
        }
        if backoff < time.Second {
            backoff *= 2
        }
    }

    return chatResponse{}, lastErr
}
