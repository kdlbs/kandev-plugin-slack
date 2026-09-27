//go:build !kandev_min_host

package main

import (
	"context"

	"github.com/kandev/kandev/pkg/pluginsdk"
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
