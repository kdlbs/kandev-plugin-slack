package main

import (
	"context"

	"github.com/kdlbs/kandev/pluginsdk"
)

func (h *fakeHost) InvokeUtilityAgent(_ context.Context, prompt string, _ ...pluginsdk.UtilityAgentOptions) (string, error) {
	h.mu.Lock()
	h.prompts = append(h.prompts, prompt)
	h.mu.Unlock()
	if h.agentErr != nil {
		return "", h.agentErr
	}
	return h.agentResponse, nil
}
