//go:build kandev_min_host

package main

import "context"

func (h *fakeHost) InvokeUtilityAgent(_ context.Context, prompt string) (string, error) {
	h.mu.Lock()
	h.prompts = append(h.prompts, prompt)
	h.mu.Unlock()
	if h.agentErr != nil {
		return "", h.agentErr
	}
	return h.agentResponse, nil
}
