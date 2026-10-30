package research

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func (m *ModelClient) readModelStream(ctx context.Context, reader io.Reader) (string, error) {
	var text strings.Builder
	complete := false
	appendText := func(delta string) error {
		if delta == "" {
			return nil
		}
		if text.Len()+len(delta) > 2<<20 {
			return errors.New("model response exceeds limit")
		}
		text.WriteString(delta)
		taskEmit(ctx, "delta", map[string]string{"text": delta})
		return nil
	}
	consume := func(raw string) error {
		if strings.TrimSpace(raw) == "[DONE]" {
			return nil
		}
		switch m.Provider {
		case "ollama":
			var v struct {
				Response string `json:"response"`
				Error    string `json:"error"`
				Done     bool   `json:"done"`
				Reason   string `json:"done_reason"`
			}
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				return err
			}
			if v.Error != "" || v.Reason == "length" {
				return errors.New("model returned an incomplete response")
			}
			if err := appendText(v.Response); err != nil {
				return err
			}
			if v.Done {
				complete = true
			}
		case "google":
			var v struct {
				Error      json.RawMessage `json:"error"`
				Candidates []struct {
					Index   int `json:"index"`
					Content struct {
						Parts []struct {
							Text    string `json:"text"`
							Thought bool   `json:"thought"`
						} `json:"parts"`
					} `json:"content"`
					Finish string `json:"finishReason"`
				} `json:"candidates"`
				Feedback struct {
					Block string `json:"blockReason"`
				} `json:"promptFeedback"`
			}
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				return err
			}
			if len(v.Error) > 0 || v.Feedback.Block != "" {
				return errors.New("model returned no complete answer")
			}
			for _, candidate := range v.Candidates {
				if candidate.Index != 0 {
					continue
				}
				for _, part := range candidate.Content.Parts {
					if !part.Thought {
						if err := appendText(part.Text); err != nil {
							return err
						}
					}
				}
				if candidate.Finish != "" {
					if candidate.Finish != "STOP" {
						return errors.New("model returned an incomplete answer")
					}
					complete = true
				}
			}
		case "compatible":
			var v struct {
				Error   json.RawMessage `json:"error"`
				Choices []struct {
					Index int `json:"index"`
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					Finish *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(raw), &v); err != nil {
				return err
			}
			if len(v.Error) > 0 {
				return errors.New("model streaming failed")
			}
			for _, choice := range v.Choices {
				if choice.Index != 0 {
					continue
				}
				if err := appendText(choice.Delta.Content); err != nil {
					return err
				}
				if choice.Finish != nil {
					if *choice.Finish != "stop" {
						return errors.New("model returned an incomplete answer")
					}
					complete = true
				}
			}
		}
		return nil
	}
	scanner := bufio.NewScanner(io.LimitReader(reader, 8<<20))
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	data := []string{}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if m.Provider == "ollama" {
			if strings.TrimSpace(line) != "" {
				if err := consume(line); err != nil {
					return "", err
				}
			}
			continue
		}
		if line == "" {
			if len(data) > 0 {
				if err := consume(strings.Join(data, "\n")); err != nil {
					return "", err
				}
				data = nil
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if len(data) > 0 {
		if err := consume(strings.Join(data, "\n")); err != nil {
			return "", err
		}
	}
	if !complete || strings.TrimSpace(text.String()) == "" {
		return "", errors.New("model stream ended before a complete answer")
	}
	return text.String(), nil
}
